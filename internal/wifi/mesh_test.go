package wifi

import (
	"testing"
)

func TestNewMeshController(t *testing.T) {
	mc := NewMeshController("wlan0")
	if mc == nil {
		t.Fatal("NewMeshController returned nil")
	}
	if mc.ifname != "wlan0" {
		t.Errorf("Expected ifname=wlan0, got %s", mc.ifname)
	}
}

func TestMeshController_JoinMesh_Validation(t *testing.T) {
	mc := NewMeshController("wlan0")

	tests := []struct {
		name    string
		ssid    string
		channel int
		wantErr bool
	}{
		{"valid params", "test-mesh", 6, false},
		{"empty SSID", "", 6, true},
		{"invalid channel low", "test-mesh", 0, true},
		{"invalid channel high", "test-mesh", 15, true},
		{"channel 1", "test-mesh", 1, false},
		{"channel 14", "test-mesh", 14, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mc.JoinMesh(tt.ssid, tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("JoinMesh() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMeshController_LeaveMesh(t *testing.T) {
	mc := NewMeshController("wlan0")
	if err := mc.LeaveMesh(); err != nil {
		t.Errorf("LeaveMesh() error = %v", err)
	}
}

func TestMeshController_Close(t *testing.T) {
	mc := NewMeshController("wlan0")
	if err := mc.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
