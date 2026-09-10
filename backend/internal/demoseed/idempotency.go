package demoseed

// OperationID builds a stable, deterministic operation identifier of the
// form "renovex-demo:v1:<projectSlug>:<stepSlug>" (design spec §6.3).
func OperationID(projectSlug, stepSlug string) string {
	return "renovex-demo:v1:" + projectSlug + ":" + stepSlug
}

// FindByName returns the first item in items whose name (per nameOf)
// equals name, and true — or the zero value and false if none match.
func FindByName[T any](items []T, name string, nameOf func(T) string) (T, bool) {
	for _, item := range items {
		if nameOf(item) == name {
			return item, true
		}
	}
	var zero T
	return zero, false
}
