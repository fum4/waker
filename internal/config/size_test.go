package config

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    Size
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "512MiB", want: 512 * MiB},
		{in: "512M", want: 512 * MiB},
		{in: "2GiB", want: 2 * GiB},
		{in: "2G", want: 2 * GiB},
		{in: "64KiB", want: 64 * KiB},
		{in: "1TiB", want: TiB},
		{in: "100B", want: 100},
		{in: " 768 MiB ", want: 768 * MiB},
		{in: "512", wantErr: true},
		{in: "512mb", wantErr: true},
		{in: "-1GiB", wantErr: true},
		{in: "1.5GiB", wantErr: true},
		{in: "", wantErr: true},
		{in: "99999999999999TiB", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSize(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSize(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSizeString(t *testing.T) {
	tests := []struct {
		in   Size
		want string
	}{
		{0, "0B"},
		{512 * MiB, "512MiB"},
		{5 * GiB, "5GiB"},
		{1536 * KiB, "1536KiB"},
		{100, "100B"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Size(%d).String() = %q, want %q", int64(tt.in), got, tt.want)
		}
	}
}

func TestSizeMiBsRoundsDown(t *testing.T) {
	if got := (MiB + MiB/2).MiBs(); got != 1 {
		t.Fatalf("1.5MiB.MiBs() = %d, want 1", got)
	}
}
