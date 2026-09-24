package links

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// schemaExtractor reads an ORM's own schema/config for the database engine
// it names, and uses it to set the engine on the component's database link —
// creating it if the dependency scan didn't (e.g. the schema ships without
// the client package in this component).
type schemaExtractor struct{}

func (schemaExtractor) extract(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("schema.prisma") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		vendor, name, protocol, line, ok := prismaEngine(ctx.tree.ReadString(file))
		if !ok {
			continue
		}
		upgradeDatabaseLink(ctx, owner, "dep:prisma", vendor, name, protocol, domain.SourceEvidence{Path: file, Line: line})
	}

	for _, file := range ctx.tree.Files {
		if !strings.HasPrefix(baseName(file), "drizzle.config") {
			continue
		}
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		vendor, name, protocol, line, ok := drizzleEngine(ctx.tree.ReadString(file))
		if !ok {
			continue
		}
		upgradeDatabaseLink(ctx, owner, "dep:drizzle", vendor, name, protocol, domain.SourceEvidence{Path: file, Line: line})
	}
}

func upgradeDatabaseLink(ctx *scanCtx, componentPath, signalKey, vendor, name string, protocol domain.LinkProtocol, evidence domain.SourceEvidence) {
	sig, ok := ctx.collector.link(componentPath, signalKey)
	if !ok {
		ctx.collector.addLink(rawSignal{
			componentPath: componentPath,
			signalKey:     signalKey,
			protocol:      protocol,
			target: domain.LinkTarget{
				Kind:         domain.LinkTargetResource,
				ResourceKind: domain.ResourceDatabase,
				Vendor:       vendor,
				Name:         name,
			},
			evidence:   []domain.SourceEvidence{evidence},
			confidence: domain.ConfidenceHigh,
			family:     familyOf(domain.ResourceDatabase, vendor),
		})
		return
	}
	sig.target.Vendor = vendor
	sig.target.Name = name
	sig.protocol = protocol
	sig.mergeEvidence([]domain.SourceEvidence{evidence})
}

var prismaDatasourceRe = regexp.MustCompile(`(?s)datasource\s+\w+\s*\{(.*?)\}`)
var prismaProviderRe = regexp.MustCompile(`provider\s*=\s*"([^"]+)"`)

func prismaEngine(content string) (vendor, name string, protocol domain.LinkProtocol, line int, ok bool) {
	block := prismaDatasourceRe.FindStringSubmatchIndex(content)
	if block == nil {
		return "", "", "", 0, false
	}
	body := content[block[2]:block[3]]
	m := prismaProviderRe.FindStringSubmatchIndex(body)
	if m == nil {
		return "", "", "", 0, false
	}
	provider := body[m[2]:m[3]]
	lineNo := 1 + strings.Count(content[:block[2]+m[0]], "\n")
	vendor, name, protocol = dbEngineFromScheme(provider)
	return vendor, name, protocol, lineNo, true
}

var drizzleDialectRe = regexp.MustCompile(`(?:dialect|driver)\s*:\s*['"]([A-Za-z0-9_-]+)['"]`)

func drizzleEngine(content string) (vendor, name string, protocol domain.LinkProtocol, line int, ok bool) {
	m := drizzleDialectRe.FindStringSubmatchIndex(content)
	if m == nil {
		return "", "", "", 0, false
	}
	value := content[m[2]:m[3]]
	lineNo := 1 + strings.Count(content[:m[0]], "\n")
	switch value {
	case "pg", "postgres", "postgresql":
		return "postgres", "PostgreSQL", domain.LinkSQL, lineNo, true
	case "mysql", "mysql2":
		return "mysql", "MySQL", domain.LinkSQL, lineNo, true
	case "sqlite", "better-sqlite3", "turso", "libsql":
		return "sqlite", "SQLite", domain.LinkSQL, lineNo, true
	}
	return "", "", "", 0, false
}

// dbEngineFromScheme maps a value's URL scheme or a schema.prisma provider
// name onto a display vendor; unrecognised inputs stay a generic database so
// the caller can still form a link without inventing a wrong vendor.
func dbEngineFromScheme(v string) (vendor, name string, protocol domain.LinkProtocol) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.HasPrefix(v, "postgres"):
		return "postgres", "PostgreSQL", domain.LinkSQL
	case strings.HasPrefix(v, "mysql"):
		return "mysql", "MySQL", domain.LinkSQL
	case strings.HasPrefix(v, "mongodb"):
		return "mongodb", "MongoDB", domain.LinkOther
	case strings.HasPrefix(v, "sqlite"):
		return "sqlite", "SQLite", domain.LinkSQL
	case strings.HasPrefix(v, "sqlserver"):
		return "mssql", "SQL Server", domain.LinkSQL
	case strings.HasPrefix(v, "cockroachdb"):
		return "cockroachdb", "CockroachDB", domain.LinkSQL
	}
	return "database", "Database", domain.LinkSQL
}
