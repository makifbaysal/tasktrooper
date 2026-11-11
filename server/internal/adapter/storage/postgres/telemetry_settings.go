package postgres

import "context"

const (
	telemetryURLKey = "telemetry_base_url"
	telemetryKeyKey = "telemetry_api_key"
)

// TelemetrySettings: the API URL in the clear, the API key decrypted with the
// boot-time cipher. Both are "" until saved.
func (s *SettingsStore) TelemetrySettings(ctx context.Context) (baseURL, apiKey string, err error) {
	baseURL, err = s.plain(ctx, telemetryURLKey)
	if err != nil {
		return "", "", err
	}
	apiKey, err = s.secret(ctx, telemetryKeyKey)
	if err != nil {
		return "", "", err
	}
	return baseURL, apiKey, nil
}

func (s *SettingsStore) SetTelemetrySettings(ctx context.Context, baseURL string, apiKey *string) error {
	if apiKey != nil {
		if *apiKey == "" {
			if err := s.forgetKeys(ctx, telemetryKeyKey); err != nil {
				return err
			}
		} else if err := s.setSecret(ctx, telemetryKeyKey, *apiKey); err != nil {
			return err
		}
	}
	if baseURL == "" {
		return s.forgetKeys(ctx, telemetryURLKey)
	}
	return s.setPlain(ctx, telemetryURLKey, baseURL)
}
