package daemonrunner

// validateDatabaseFingerprint was used in legacy versions to ensure the
// database matched the current repository fingerprint. Modern beads releases
// rely on config.json-driven multi-repo metadata instead, so the validation is
// now a no-op placeholder to keep older call sites simple.
func (d *Daemon) validateDatabaseFingerprint() error {
	return nil
}
