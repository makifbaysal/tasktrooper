package datasource

import (
	"context"
	"errors"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// storeProviderAliases: the console calls App Store Connect's credential
// "asc"; a request naming it in full means the same one.
var storeProviderAliases = map[string]string{"app_store_connect": domain.StoreCredentialASC}

func canonicalStoreProvider(v string) string {
	if c, ok := storeProviderAliases[strings.ToLower(v)]; ok {
		return c
	}
	return v
}

// checkStoreCredential: the value is the provider whose credential the
// source reads; it must be the field's provider and that credential must be
// saved. Without a reader, or on a field naming no provider, the field takes
// no value.
func (s *Service) checkStoreCredential(ctx context.Context, field domain.DataSourceField, v string) (string, error) {
	if s.storeCreds == nil || field.Provider == "" {
		return FieldNotPermitted, nil
	}
	if want := string(field.Provider); v != want {
		if v == domain.StoreCredentialASC || v == domain.StoreCredentialGooglePlay {
			return FieldWrongProvider, nil
		}
		return FieldInvalid, nil
	}
	_, err := s.storeCreds.StoreCredential(ctx, v)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return FieldNotFound, nil
	case err != nil:
		return "", err
	}
	return "", nil
}

// storeCredentialMasks adds the values of every store credential the config
// names to out, keyed apart from the source's own secrets.
func (s *Service) storeCredentialMasks(ctx context.Context, schema domain.DataSourceSchema, config, out map[string]string) {
	if s.storeCreds == nil {
		return
	}
	for _, f := range schema.Fields {
		if f.Type != domain.FieldStoreCredential || config[f.Key] == "" {
			continue
		}
		cred, err := s.storeCreds.StoreCredential(ctx, config[f.Key])
		if err != nil {
			continue
		}
		for k, v := range cred.Data {
			out["store_credential:"+f.Key+":"+k] = v
		}
	}
}

// StoreCredentialConfig is the config a new source of kind needs for its
// store_credential fields (each field's key → its provider), and the
// provider of the first, for a detector offering to create one. ok is false
// for a kind that reads no store credential or is not registered.
func (s *Service) StoreCredentialConfig(kind domain.DataSourceKind) (config map[string]string, provider string, ok bool) {
	adapter, registered := s.kinds[kind]
	if !registered {
		return nil, "", false
	}
	config = map[string]string{}
	for _, f := range adapter.Schema().Fields {
		if f.Type == domain.FieldStoreCredential && f.Provider != "" {
			config[f.Key] = string(f.Provider)
			if provider == "" {
				provider = string(f.Provider)
			}
		}
	}
	return config, provider, provider != ""
}

// SetStoreCredentials is Deps.StoreCredentials for a reader built after the
// service (the store ops service, wired later at boot); call it only before
// serving.
func (s *Service) SetStoreCredentials(r port.StoreCredentialReader) {
	s.storeCreds = r
	for _, k := range s.kinds {
		if setter, ok := k.(StoreCredentialsSetter); ok {
			setter.SetStoreCredentials(r)
		}
	}
}
