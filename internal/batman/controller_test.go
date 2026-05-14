package batman

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNewController_Disabled(t *testing.T) {
	c, err := NewController("bat0", "wlan0", false)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	if !c.IsFallbackMode() {
		t.Error("Expected fallback mode when disabled")
	}
}

func TestNewController_FallbackWhenModuleMissing(t *testing.T) {
	// This test simulates missing batman-adv module
	// In real environments, /sys/module/batman_adv may not exist
	c, err := NewController("bat0", "wlan0", true)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	// If batman-adv module is not loaded, should be in fallback mode
	if _, err := os.Stat("/sys/module/batman_adv"); os.IsNotExist(err) {
		if !c.IsFallbackMode() {
			t.Error("Expected fallback mode when batman-adv module missing")
		}
	}
}

func TestController_SubscribeOGMEvents(t *testing.T) {
	c := &Controller{
		batInterface:  "bat0",
		meshInterface: "wlan0",
		enabled:       true,
		fallbackMode:  true, // Test in fallback mode for simplicity
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.SubscribeOGMEvents(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("SubscribeOGMEvents returned unexpected error: %v", err)
	}
}

func TestController_IsFallbackMode(t *testing.T) {
	tests := []struct {
		name         string
		fallbackMode bool
		want         bool
	}{
		{"fallback enabled", true, true},
		{"fallback disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{fallbackMode: tt.fallbackMode}
			if got := c.IsFallbackMode(); got != tt.want {
				t.Errorf("IsFallbackMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
