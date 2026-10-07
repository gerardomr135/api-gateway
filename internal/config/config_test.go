package config

import "testing"

// TestValidate is a table-driven test skeleton. Each case is a Config and
// whether Validate should reject it. Add a case for every check you implement.
func TestValidate(t *testing.T) {
	t.Skip("TODO(M2): remove this Skip when you start implementing Validate")

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "route references unknown upstream",
			cfg: Config{
				Upstreams: map[string]Upstream{},
				Routes:    []Route{{Name: "users", Prefix: "/api/users", Upstream: "users"}},
			},
			wantErr: true,
		},
		// TODO(M2): duplicate prefix, bad instance URL, prefix without leading "/",
		// invalid method, and one fully valid config (wantErr: false).
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
