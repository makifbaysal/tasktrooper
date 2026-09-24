package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultLogLimit  = 200
	maxLogLimit      = 1000
	defaultLogWindow = 24 * time.Hour
)

// logTarget is the CloudWatch Logs group (and, for ECS, the awslogs stream
// prefix that scopes results to the one container the task definition names)
// a resource's logs resolve to.
type logTarget struct {
	group        string
	streamPrefix string
}

func (p *Provider) Logs(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	cfg, err := p.config(cred)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	target, err := p.resolveLogTarget(ctx, cfg, ref)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	end := q.Until
	if end.IsZero() {
		end = p.now()
	}
	start := q.Since
	if start.IsZero() {
		start = end.Add(-defaultLogWindow)
	}

	pattern, filterSeverityClientSide := buildFilterPattern(q.Text, q.MinSeverity)

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(target.group),
		StartTime:    aws.Int64(start.UnixMilli()),
		EndTime:      aws.Int64(end.UnixMilli()),
		Limit:        aws.Int32(int32(limit)),
	}
	if pattern != "" {
		input.FilterPattern = aws.String(pattern)
	}
	if target.streamPrefix != "" {
		input.LogStreamNamePrefix = aws.String(target.streamPrefix)
	}
	if q.Cursor != "" {
		input.NextToken = aws.String(q.Cursor)
	}

	out, err := p.logsClient(cfg).FilterLogEvents(ctx, input)
	if err != nil {
		if errorCode(err) == "ResourceNotFoundException" {
			return domain.RuntimeLogPage{}, nil
		}
		return domain.RuntimeLogPage{}, classify("cloudwatch logs filter log events", err)
	}

	entries := make([]domain.RuntimeLogEntry, 0, len(out.Events))
	for _, e := range out.Events {
		entry := domain.RuntimeLogEntry{
			Timestamp: msToTime(aws.ToInt64(e.Timestamp)),
			Message:   aws.ToString(e.Message),
			Source:    aws.ToString(e.LogStreamName),
		}
		entry.Severity = inferSeverity(entry.Message)
		if filterSeverityClientSide && entry.Severity.Rank() < q.MinSeverity.Rank() {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })

	return domain.RuntimeLogPage{
		Entries:    entries,
		NextCursor: aws.ToString(out.NextToken),
	}, nil
}

func (p *Provider) resolveLogTarget(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (logTarget, error) {
	switch ref.Kind {
	case domain.CloudResourceLambdaFunction:
		if g := ref.Extra[extraLogGroup]; g != "" {
			return logTarget{group: g}, nil
		}
		return logTarget{group: "/aws/lambda/" + ref.Name}, nil

	case domain.CloudResourceECSService:
		return p.resolveECSLogTarget(ctx, cfg, ref)

	case domain.CloudResourceAppRunnerService:
		serviceID := ref.Extra[extraServiceID]
		if serviceID == "" {
			return logTarget{}, fmt.Errorf("aws: app runner service %q has no service id recorded", ref.Name)
		}
		return logTarget{group: fmt.Sprintf("/aws/apprunner/%s/%s/application", ref.Name, serviceID)}, nil

	default:
		return logTarget{}, fmt.Errorf("%w: aws logs unsupported for %s", port.ErrUnsupported, ref.Kind)
	}
}

func (p *Provider) resolveECSLogTarget(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (logTarget, error) {
	taskDef := ref.Extra[extraTaskDefinition]
	if taskDef == "" {
		return logTarget{}, fmt.Errorf("aws: ecs service %q has no task definition recorded", ref.Name)
	}
	out, err := p.ecsClient(cfg).DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDef),
	})
	if err != nil {
		return logTarget{}, classify("ecs describe task definition", err)
	}
	if out.TaskDefinition == nil {
		return logTarget{}, fmt.Errorf("aws: task definition %q has no definition in the response", taskDef)
	}
	for _, c := range out.TaskDefinition.ContainerDefinitions {
		if c.LogConfiguration == nil || c.LogConfiguration.LogDriver != "awslogs" {
			continue
		}
		group := c.LogConfiguration.Options["awslogs-group"]
		if group == "" {
			continue
		}
		return logTarget{group: group, streamPrefix: c.LogConfiguration.Options["awslogs-stream-prefix"]}, nil
	}
	return logTarget{}, fmt.Errorf("aws: task definition %q has no container logging to CloudWatch via awslogs", taskDef)
}

const (
	errorSeverityGroup = `?ERROR ?Error ?error ?Exception ?FATAL ?panic`
	warnSeverityGroup  = `?WARN ?Warn ?warning`
)

// buildFilterPattern returns the CloudWatch FilterPattern to send and
// whether MinSeverity must additionally be enforced client-side. CloudWatch's
// filter pattern syntax can AND several quoted terms or OR a bracketed group
// of tokens, but not reliably AND a literal phrase with an OR-group of
// severity tokens — so when both a text search and a severity floor are
// given, only the text goes server-side and severity is checked per entry
// after the fetch.
func buildFilterPattern(text string, minSeverity domain.LogSeverity) (pattern string, filterSeverityClientSide bool) {
	text = strings.TrimSpace(text)
	severityGroup := severityFilterGroup(minSeverity)
	switch {
	case text != "" && severityGroup != "":
		return quoteFilterTerm(text), true
	case text != "":
		return quoteFilterTerm(text), false
	case severityGroup != "":
		return severityGroup, false
	default:
		return "", false
	}
}

func severityFilterGroup(min domain.LogSeverity) string {
	switch min {
	case domain.LogError:
		return errorSeverityGroup
	case domain.LogWarning:
		return errorSeverityGroup + " " + warnSeverityGroup
	default:
		return ""
	}
}

var filterTermEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func quoteFilterTerm(s string) string {
	return `"` + filterTermEscaper.Replace(s) + `"`
}

// inferSeverity reads a level out of a JSON log line's level/severity field,
// falls back to the line's leading token (ERROR, WARN, INFO, DEBUG, FATAL,
// panic — case-insensitively, since drivers and app frameworks disagree on
// casing), and defaults to info when neither is present.
func inferSeverity(message string) domain.LogSeverity {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return domain.LogInfo
	}
	if strings.HasPrefix(trimmed, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			for k, v := range payload {
				if !strings.EqualFold(k, "level") && !strings.EqualFold(k, "severity") {
					continue
				}
				if s, ok := v.(string); ok {
					if sev, ok := severityFromToken(s); ok {
						return sev
					}
				}
			}
		}
	}
	if fields := strings.Fields(trimmed); len(fields) > 0 {
		token := strings.Trim(fields[0], "[]:,")
		if sev, ok := severityFromToken(token); ok {
			return sev
		}
	}
	return domain.LogInfo
}

func severityFromToken(tok string) (domain.LogSeverity, bool) {
	switch strings.ToUpper(tok) {
	case "ERROR":
		return domain.LogError, true
	case "WARN", "WARNING":
		return domain.LogWarning, true
	case "INFO":
		return domain.LogInfo, true
	case "DEBUG":
		return domain.LogDebug, true
	case "FATAL", "CRITICAL", "PANIC":
		return domain.LogCritical, true
	default:
		return "", false
	}
}

func msToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
