package node

import "testing"

// Hilfen für den Ende-zu-Ende-Test mit dem Panel (Paket node_test). Sie gibt es nur in Tests.

// NewManagerForTest baut einen Manager mit Docker-Attrappe und Software ohne Netz.
func NewManagerForTest(t *testing.T, cfg Config) (*Manager, Runtime) {
	rt := newFakeRuntime()
	return newTestManager(t, cfg, rt), rt
}

// ConfigForTest liefert eine Config mit eigenem Datenordner.
func ConfigForTest(t *testing.T) Config { return testConfig(t) }

// SoftwareForTest lädt nichts aus dem Netz.
var SoftwareForTest Software = fakeSoftware{}

// HostKeyForTest ist der öffentliche Host-Schlüssel des Dienstes.
func (s *SFTPService) HostKeyForTest() []byte { return s.signer.PublicKey().Marshal() }
