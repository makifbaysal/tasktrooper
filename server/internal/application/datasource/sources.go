package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	verifyTimeout      = 20 * time.Second
	maxNameLen         = 200
	maxValueLen        = 4096
	maxHeadersLen      = 16384
	maxStatusDetailLen = 500
	redactedMarker     = "[redacted]"
)

func (s *Service) List(ctx context.Context) ([]domain.DataSource, error) {
	list, err := s.store.ListDataSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DataSource, 0, len(list))
	for _, ds := range list {
		out = append(out, s.present(ds))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.DataSource, error) {
	ds, err := s.store.GetDataSource(ctx, id)
	if err != nil {
		return domain.DataSource{}, err
	}
	return s.present(ds), nil
}

// Create stores the source first and verifies it after, so a source that
// cannot be reached yet is kept (status "error") rather than refused; only a
// request that does not fit the kind's schema is.
func (s *Service) Create(ctx context.Context, req domain.SaveDataSourceRequest) (domain.DataSource, error) {
	adapter, ok := s.kinds[req.Kind]
	if !ok {
		return domain.DataSource{}, FieldErrors{"kind": FieldUnknown}
	}
	schema := adapter.Schema()
	config, secrets, fe, err := s.merge(ctx, schema, nil, nil, req.Config, req.Secrets)
	if err != nil {
		return domain.DataSource{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = s.defaultName(ctx, schema, config)
	}
	if len(name) > maxNameLen {
		fe = fe.with("name", FieldTooLong)
	}
	if len(fe) > 0 {
		return domain.DataSource{}, fe
	}
	created, err := s.store.CreateDataSource(ctx, domain.DataSource{
		Kind:   req.Kind,
		Name:   name,
		Config: config,
		Status: domain.DataSourceUnverified,
	}, secrets)
	if err != nil {
		return domain.DataSource{}, err
	}
	return s.verifyAndRecord(ctx, adapter, created, secrets)
}

// Update merges the patch into the stored source, re-validates the whole
// result against the schema, saves, and verifies again.
func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.PatchDataSourceRequest) (domain.DataSource, error) {
	ds, err := s.store.GetDataSource(ctx, id)
	if err != nil {
		return domain.DataSource{}, err
	}
	adapter, ok := s.kinds[ds.Kind]
	if !ok {
		return domain.DataSource{}, fmt.Errorf("kind %q: %w", ds.Kind, ErrUnsupported)
	}
	stored, err := s.store.DataSourceSecrets(ctx, id)
	if err != nil {
		return domain.DataSource{}, err
	}
	schema := adapter.Schema()
	config, secrets, fe, err := s.merge(ctx, schema, ds.Config, stored, req.Config, req.Secrets)
	if err != nil {
		return domain.DataSource{}, err
	}
	if carriesSecretsToANewOrigin(schema, ds.Config, config, stored, secrets, req) {
		fe = fe.with("secrets", FieldSecretsRequired)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		switch {
		case name == "":
			fe = fe.with("name", FieldRequired)
		case len(name) > maxNameLen:
			fe = fe.with("name", FieldTooLong)
		default:
			ds.Name = name
		}
	}
	if len(fe) > 0 {
		return domain.DataSource{}, fe
	}
	ds.Config = config
	ds.Status = domain.DataSourceUnverified
	ds.StatusDetail = ""
	saved, err := s.store.UpdateDataSource(ctx, ds, secrets)
	if err != nil {
		return domain.DataSource{}, err
	}
	s.changed(id)
	return s.verifyAndRecord(ctx, adapter, saved, secrets)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDataSource(ctx, id); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// Verify re-checks a stored source. A failure is recorded in Status and
// StatusDetail, not returned, so a caller reads it like any other source.
func (s *Service) Verify(ctx context.Context, id uuid.UUID) (domain.DataSource, error) {
	ds, adapter, secrets, err := s.load(ctx, id)
	if err != nil {
		return domain.DataSource{}, err
	}
	return s.verifyAndRecord(ctx, adapter, ds, secrets)
}

func (s *Service) load(ctx context.Context, id uuid.UUID) (domain.DataSource, port.DataSourceKindAdapter, map[string]string, error) {
	ds, err := s.store.GetDataSource(ctx, id)
	if err != nil {
		return domain.DataSource{}, nil, nil, err
	}
	adapter, ok := s.kinds[ds.Kind]
	if !ok {
		return domain.DataSource{}, nil, nil, fmt.Errorf("kind %q: %w", ds.Kind, ErrUnsupported)
	}
	secrets, err := s.store.DataSourceSecrets(ctx, id)
	if err != nil {
		return domain.DataSource{}, nil, nil, err
	}
	return ds, adapter, secrets, nil
}

// verifyAndRecord writes only the outcome, guarded by the snapshot's
// updated_at: a verify that finishes after a newer PATCH must not put the
// config it ran against back, nor stamp its status on the new one. When the
// source moved on, the caller gets what is stored now.
func (s *Service) verifyAndRecord(ctx context.Context, adapter port.DataSourceKindAdapter, ds domain.DataSource, secrets map[string]string) (domain.DataSource, error) {
	vctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	info, verr := adapter.Verify(vctx, ds, secrets)
	cancel()
	v := domain.DataSourceVerification{Status: domain.DataSourceOK, Info: info}
	if verr != nil {
		v = domain.DataSourceVerification{
			Status:       domain.DataSourceError,
			StatusDetail: truncate(redact(verr.Error(), s.masks(ctx, adapter.Schema(), ds.Config, secrets)), maxStatusDetailLen),
		}
	} else {
		now := s.now()
		v.VerifiedAt = &now
	}
	// Recorded even when the request that asked for it was cancelled
	// meanwhile: the source was saved, so its status must not stay at
	// whatever it said before.
	wctx := context.WithoutCancel(ctx)
	saved, err := s.store.RecordVerification(wctx, ds.ID, v, ds.UpdatedAt)
	if errors.Is(err, port.ErrStaleDataSource) {
		saved, err = s.store.GetDataSource(wctx, ds.ID)
	}
	if err != nil {
		return domain.DataSource{}, err
	}
	return s.present(saved), nil
}

// carriesSecretsToANewOrigin: a PATCH that points a url field at another
// scheme, host or port would otherwise send the stored token, password or
// headers there without the person typing them again. Every stored secret the
// merged result still holds must have come in with the request (a new value;
// "" removes it and so never reaches this check).
func carriesSecretsToANewOrigin(schema domain.DataSourceSchema, oldConfig, newConfig, stored, merged map[string]string, req domain.PatchDataSourceRequest) bool {
	moved := false
	for _, f := range schema.Fields {
		if f.Type == domain.FieldURL && oldConfig[f.Key] != "" && origin(oldConfig[f.Key]) != origin(newConfig[f.Key]) {
			moved = true
		}
		if f.Destination && oldConfig[f.Key] != "" && hostSet(oldConfig[f.Key]) != hostSet(newConfig[f.Key]) {
			moved = true
		}
	}
	if !moved {
		return false
	}
	for key := range stored {
		if _, kept := merged[key]; !kept {
			continue
		}
		_, inSecrets := req.Secrets[key]
		_, inConfig := req.Config[key]
		if !inSecrets && !inConfig {
			return true
		}
	}
	return false
}

func origin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme + "://" + strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") + ":" + port
}

func (s *Service) present(ds domain.DataSource) domain.DataSource {
	if ds.Config == nil {
		ds.Config = map[string]string{}
	}
	ds.SecretsSet = nonNilSlice(ds.SecretsSet)
	ds.Capabilities = []domain.Capability{}
	if adapter, ok := s.kinds[ds.Kind]; ok {
		ds.Capabilities = nonNilSlice(adapter.Capabilities())
	}
	return ds
}

// merge applies an incoming config/secrets patch onto stored values and
// validates the result. Every key is routed by the schema rather than by
// which map it arrived in, so a secret sent as config is still never stored
// in plain text. A field hidden by its ShowWhen loses any value it had.
func (s *Service) merge(ctx context.Context, schema domain.DataSourceSchema, storedConfig, storedSecrets, inConfig, inSecrets map[string]string) (map[string]string, map[string]string, FieldErrors, error) {
	config := maps.Clone(storedConfig)
	if config == nil {
		config = map[string]string{}
	}
	secrets := maps.Clone(storedSecrets)
	if secrets == nil {
		secrets = map[string]string{}
	}
	var fe FieldErrors
	apply := func(in map[string]string) {
		for _, key := range sortedKeys(in) {
			field, ok := schema.Field(key)
			if !ok {
				fe = fe.with(key, FieldUnknown)
				continue
			}
			target := config
			if field.IsSecret() {
				target = secrets
			}
			v := normalizeValue(field, in[key])
			if v == "" {
				delete(target, key)
				continue
			}
			target[key] = v
		}
	}
	apply(inConfig)
	apply(inSecrets)
	for _, m := range []map[string]string{config, secrets} {
		for key := range m {
			if _, ok := schema.Field(key); !ok {
				delete(m, key)
			}
		}
	}
	for _, field := range schema.Fields {
		if field.Default != "" && !field.IsSecret() && config[field.Key] == "" {
			config[field.Key] = normalizeValue(field, field.Default)
		}
	}

	values := maps.Clone(config)
	maps.Copy(values, secrets)
	for _, field := range schema.Fields {
		if !field.Shown(values) {
			delete(config, field.Key)
			delete(secrets, field.Key)
			continue
		}
		v := values[field.Key]
		code, err := s.checkField(ctx, field, v)
		if err != nil {
			return nil, nil, nil, err
		}
		if code != "" {
			fe = fe.with(field.Key, code)
		}
	}
	return config, secrets, fe, nil
}

func normalizeValue(field domain.DataSourceField, v string) string {
	v = strings.TrimSpace(v)
	switch field.Type {
	case domain.FieldURL:
		v = strings.TrimRight(v, "/")
	case domain.FieldCloudAccount:
		if id, err := uuid.Parse(v); err == nil {
			v = id.String()
		}
	case domain.FieldStoreCredential:
		v = canonicalStoreProvider(v)
	}
	return v
}

// checkField answers an error only when the check itself could not run (the
// cloud account store failing); a value that does not fit is a code.
func (s *Service) checkField(ctx context.Context, field domain.DataSourceField, v string) (string, error) {
	if v == "" {
		if field.Required {
			return FieldRequired, nil
		}
		return "", nil
	}
	limit := maxValueLen
	if field.Type == domain.FieldHeaders {
		limit = maxHeadersLen
	}
	if len(v) > limit {
		return FieldTooLong, nil
	}
	if field.Destination {
		return s.checkHosts(v), nil
	}
	switch field.Type {
	case domain.FieldSelect:
		for _, o := range field.Options {
			if v == o {
				return "", nil
			}
		}
		return FieldInvalidOption, nil
	case domain.FieldURL:
		return s.checkURL(v), nil
	case domain.FieldHeaders:
		if _, err := domain.ParseHeaderLines(v); err != nil {
			return FieldInvalid, nil
		}
	case domain.FieldCloudAccount:
		_, code, err := s.cloudAccount(ctx, field, v)
		return code, err
	case domain.FieldStoreCredential:
		return s.checkStoreCredential(ctx, field, v)
	}
	return "", nil
}

func (s *Service) cloudAccount(ctx context.Context, field domain.DataSourceField, v string) (domain.CloudAccount, string, error) {
	id, err := uuid.Parse(v)
	if err != nil {
		return domain.CloudAccount{}, FieldInvalid, nil
	}
	if s.clouds == nil {
		return domain.CloudAccount{}, FieldNotPermitted, nil
	}
	acct, err := s.clouds.GetCloudAccount(ctx, id)
	if errors.Is(err, port.ErrNotFound) {
		return domain.CloudAccount{}, FieldNotFound, nil
	}
	if err != nil {
		return domain.CloudAccount{}, "", err
	}
	if field.Provider != "" && acct.Provider != field.Provider {
		return domain.CloudAccount{}, FieldWrongProvider, nil
	}
	return acct, "", nil
}

// masks is secrets plus the credential values of every cloud account and
// store credential the config names, for masking only: a kind reads that credential itself and
// must not echo it, and a message that does is masked like one of the
// source's own secrets. A value the account also reports in its non-secret
// Meta (an AWS region) is not one.
func (s *Service) masks(ctx context.Context, schema domain.DataSourceSchema, config, secrets map[string]string) map[string]string {
	out := maps.Clone(secrets)
	if out == nil {
		out = map[string]string{}
	}
	s.storeCredentialMasks(ctx, schema, config, out)
	if s.clouds == nil {
		return out
	}
	for _, f := range schema.Fields {
		if f.Type != domain.FieldCloudAccount {
			continue
		}
		id, err := uuid.Parse(config[f.Key])
		if err != nil {
			continue
		}
		cred, err := s.clouds.CloudCredential(ctx, id)
		if err != nil {
			continue
		}
		public := map[string]bool{}
		for _, v := range cred.Meta {
			public[v] = true
		}
		for k, v := range cred.Fields {
			if !public[v] {
				out["cloud_account:"+f.Key+":"+k] = v
			}
		}
	}
	return out
}

// checkURL refuses credentials in the URL itself: config is stored and
// returned in plain text, so a user:password@ would leak where a secret field
// would not.
func (s *Service) checkURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return FieldInvalid
	}
	if _, err := s.policy.Precheck(raw); err != nil {
		return FieldNotPermitted
	}
	return ""
}

// checkHosts is checkURL for a destination field: every host of the list is
// judged by the policy's literal rules.
func (s *Service) checkHosts(raw string) string {
	hosts, err := domain.ParseHostList(raw, "")
	if err != nil {
		return FieldInvalid
	}
	for _, h := range hosts {
		if s.policy.PrecheckHost(h) != nil {
			return FieldNotPermitted
		}
	}
	return ""
}

// hostSet is a destination field's hosts in a comparable form: order and
// case do not move a source anywhere.
func hostSet(raw string) string {
	hosts, err := domain.ParseHostList(raw, "")
	if err != nil {
		return raw
	}
	slices.Sort(hosts)
	return strings.Join(hosts, ",")
}

// defaultName is the URL's host (a destination field's first host), or for a
// kind reading a cloud account the kind's label and the account's.
func (s *Service) defaultName(ctx context.Context, schema domain.DataSourceSchema, config map[string]string) string {
	for _, f := range schema.Fields {
		if f.Destination {
			if hosts, err := domain.ParseHostList(config[f.Key], ""); err == nil {
				return hosts[0]
			}
		}
		switch f.Type {
		case domain.FieldURL:
			if u, err := url.Parse(config[f.Key]); err == nil && u.Host != "" {
				return u.Host
			}
		case domain.FieldCloudAccount:
			if acct, code, err := s.cloudAccount(ctx, f, config[f.Key]); err == nil && code == "" && acct.Label != "" {
				return schema.Label + " · " + acct.Label
			}
		}
	}
	return schema.Label
}

func (f FieldErrors) with(key, code string) FieldErrors {
	if f == nil {
		f = FieldErrors{}
	}
	if _, ok := f[key]; !ok {
		f[key] = code
	}
	return f
}

const minRedactLen = 4

// redact is a second line behind the adapters' own rule of never echoing a
// secret: a value seen in an upstream message is masked before it is stored
// or returned. The longest value goes first, so one inside another (a key's
// private_key_id inside the key file) cannot leave the rest showing. An
// adapter bounds its text before this runs, so a message cut short (ending
// in "…") also loses a tail that begins a secret.
func redact(msg string, secrets map[string]string) string {
	values := secretValues(secrets)
	for _, v := range values {
		msg = strings.ReplaceAll(msg, v, redactedMarker)
	}
	body, cut := strings.CutSuffix(msg, "…")
	if !cut {
		return msg
	}
	for i := range body {
		tail := body[i:]
		if len(tail) < minRedactLen {
			break
		}
		for _, v := range values {
			if strings.HasPrefix(v, tail) {
				return body[:i] + redactedMarker + "…"
			}
		}
	}
	return msg
}

// secretValues are the values to mask, longest first: each secret, the
// values of a headers secret, and the key material inside a service account
// key file (as parsed and as the file writes it).
func secretValues(secrets map[string]string) []string {
	var out []string
	add := func(v string) {
		if len(v) >= minRedactLen && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	for _, v := range secrets {
		add(v)
		if lines, err := domain.ParseHeaderLines(v); err == nil {
			for _, l := range lines {
				add(l.Value)
			}
		}
		var key struct {
			PrivateKey   string `json:"private_key"`
			PrivateKeyID string `json:"private_key_id"`
		}
		if json.Unmarshal([]byte(v), &key) == nil {
			add(key.PrivateKeyID)
			add(key.PrivateKey)
			add(strings.ReplaceAll(key.PrivateKey, "\n", `\n`))
			for _, line := range strings.Split(key.PrivateKey, "\n") {
				if line = strings.TrimSpace(line); !strings.HasPrefix(line, "-----") {
					add(line)
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b string) int { return len(b) - len(a) })
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// upstreamError keeps the adapter's message, redacted, while answering
// errors.Is for the one class the HTTP layer maps: a query the source
// refused (the caller's mistake) or anything else the source failed at.
type upstreamError struct {
	class error
	msg   string
}

func (e *upstreamError) Error() string        { return e.msg }
func (e *upstreamError) Is(target error) bool { return target == e.class }

func upstream(err error, secrets map[string]string) error {
	class := ErrUpstream
	switch {
	case errors.Is(err, port.ErrDataSourceQuery):
		class = port.ErrDataSourceQuery
	case errors.Is(err, port.ErrSignalNotEnabled):
		class = port.ErrSignalNotEnabled
	}
	return &upstreamError{class: class, msg: truncate(redact(err.Error(), secrets), maxStatusDetailLen)}
}
