package fix

// MergeDriver is a no-op now that the 3-way merge engine has been removed.
// Dolt handles sync natively without a git merge driver.
// The function signature is preserved to avoid breaking callers.
func MergeDriver(_ string) error {
	return nil
}
