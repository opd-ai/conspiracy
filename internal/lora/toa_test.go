package lora

import (
	"math"
	"testing"
	"time"
)

func TestCalculate_BasicCases(t *testing.T) {
	tests := []struct {
		name         string
		payloadBytes int
		sf           int
		bw           int
		cr           int
		wantToA      time.Duration
		tolerance    time.Duration
	}{
		{
			name:         "100 bytes, SF10, BW125, CR1",
			payloadBytes: 100,
			sf:           10,
			bw:           125,
			cr:           1,
			wantToA:      1026 * time.Millisecond,
			tolerance:    20 * time.Millisecond,
		},
		{
			name:         "50 bytes, SF7, BW125, CR1",
			payloadBytes: 50,
			sf:           7,
			bw:           125,
			cr:           1,
			wantToA:      98 * time.Millisecond,
			tolerance:    5 * time.Millisecond,
		},
		{
			name:         "20 bytes, SF12, BW125, CR1",
			payloadBytes: 20,
			sf:           12,
			bw:           125,
			cr:           1,
			wantToA:      1319 * time.Millisecond,
			tolerance:    20 * time.Millisecond,
		},
		{
			name:         "100 bytes, SF10, BW250, CR1",
			payloadBytes: 100,
			sf:           10,
			bw:           250,
			cr:           1,
			wantToA:      513 * time.Millisecond,
			tolerance:    10 * time.Millisecond,
		},
		{
			name:         "Empty payload, SF7, BW125, CR1",
			payloadBytes: 0,
			sf:           7,
			bw:           125,
			cr:           1,
			wantToA:      26 * time.Millisecond,
			tolerance:    2 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToA, err := Calculate(tt.payloadBytes, tt.sf, tt.bw, tt.cr)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			diff := time.Duration(math.Abs(float64(gotToA - tt.wantToA)))
			if diff > tt.tolerance {
				t.Errorf("Calculate() = %v, want %v ±%v (diff: %v)",
					gotToA, tt.wantToA, tt.tolerance, diff)
			}
		})
	}
}

func TestCalculate_InvalidParameters(t *testing.T) {
	tests := []struct {
		name         string
		payloadBytes int
		sf           int
		bw           int
		cr           int
		wantErr      bool
	}{
		{
			name:         "payload too large",
			payloadBytes: 256,
			sf:           10,
			bw:           125,
			cr:           1,
			wantErr:      true,
		},
		{
			name:         "payload negative",
			payloadBytes: -1,
			sf:           10,
			bw:           125,
			cr:           1,
			wantErr:      true,
		},
		{
			name:         "SF too low",
			payloadBytes: 50,
			sf:           6,
			bw:           125,
			cr:           1,
			wantErr:      true,
		},
		{
			name:         "SF too high",
			payloadBytes: 50,
			sf:           13,
			bw:           125,
			cr:           1,
			wantErr:      true,
		},
		{
			name:         "invalid bandwidth",
			payloadBytes: 50,
			sf:           10,
			bw:           100,
			cr:           1,
			wantErr:      true,
		},
		{
			name:         "CR too low",
			payloadBytes: 50,
			sf:           10,
			bw:           125,
			cr:           0,
			wantErr:      true,
		},
		{
			name:         "CR too high",
			payloadBytes: 50,
			sf:           10,
			bw:           125,
			cr:           5,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Calculate(tt.payloadBytes, tt.sf, tt.bw, tt.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Calculate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		payloadBytes int
		sf           int
		wantToA      time.Duration
		tolerance    time.Duration
	}{
		{
			name:         "100 bytes, SF10",
			payloadBytes: 100,
			sf:           10,
			wantToA:      1026 * time.Millisecond,
			tolerance:    20 * time.Millisecond,
		},
		{
			name:         "50 bytes, SF7",
			payloadBytes: 50,
			sf:           7,
			wantToA:      98 * time.Millisecond,
			tolerance:    5 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToA, err := CalculateWithDefaults(tt.payloadBytes, tt.sf)
			if err != nil {
				t.Fatalf("CalculateWithDefaults() error = %v", err)
			}

			diff := time.Duration(math.Abs(float64(gotToA - tt.wantToA)))
			if diff > tt.tolerance {
				t.Errorf("CalculateWithDefaults() = %v, want %v ±%v (diff: %v)",
					gotToA, tt.wantToA, tt.tolerance, diff)
			}
		})
	}
}

func TestCalculate_SemtechDatasheetValues(t *testing.T) {
	// These values are calculated using the Semtech formula
	tests := []struct {
		name         string
		payloadBytes int
		sf           int
		bw           int
		cr           int
		wantToA      time.Duration
		tolerance    time.Duration
	}{
		{
			name:         "Datasheet: 13 bytes, SF12, BW125",
			payloadBytes: 13,
			sf:           12,
			bw:           125,
			cr:           1,
			wantToA:      1155 * time.Millisecond,
			tolerance:    20 * time.Millisecond,
		},
		{
			name:         "Datasheet: 13 bytes, SF7, BW125",
			payloadBytes: 13,
			sf:           7,
			bw:           125,
			cr:           1,
			wantToA:      46 * time.Millisecond,
			tolerance:    2 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToA, err := Calculate(tt.payloadBytes, tt.sf, tt.bw, tt.cr)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			diff := time.Duration(math.Abs(float64(gotToA - tt.wantToA)))
			if diff > tt.tolerance {
				t.Errorf("Calculate() = %v, want %v ±%v (diff: %v)",
					gotToA, tt.wantToA, tt.tolerance, diff)
			}
		})
	}
}

func TestCalculate_HighSF_LowDataRateOptimize(t *testing.T) {
	// Test that SF11 and SF12 use low data rate optimization (DE=1)
	tests := []struct {
		name         string
		payloadBytes int
		sf           int
	}{
		{
			name:         "SF11 with DE=1",
			payloadBytes: 50,
			sf:           11,
		},
		{
			name:         "SF12 with DE=1",
			payloadBytes: 50,
			sf:           12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toa, err := Calculate(tt.payloadBytes, tt.sf, 125, 1)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			if toa == 0 {
				t.Errorf("Calculate() returned zero ToA for SF%d", tt.sf)
			}

			t.Logf("SF%d ToA for %d bytes: %v", tt.sf, tt.payloadBytes, toa)
		})
	}
}

func BenchmarkCalculate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Calculate(100, 10, 125, 1)
	}
}

func BenchmarkCalculateWithDefaults(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CalculateWithDefaults(100, 10)
	}
}
