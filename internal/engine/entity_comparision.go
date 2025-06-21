package engine

type Comparable interface {
	*GithubTeamComparable | *GithubRepoComparable | *GithubRuleSet | *GithubBranchProtection | *GithubEnvironment | *GithubAutolink
}

type CompareEqualAB[A Comparable, B Comparable] func(key string, value1 A, value2 B) bool

type CompareCallback[A Comparable, B Comparable] func(key string, value1 A, value2 B)

func CompareEntities[A Comparable, B Comparable](a map[string]A, b map[string]B, compareFunction CompareEqualAB[A, B], onAdded CompareCallback[A, B], onRemoved CompareCallback[A, B], onChanged CompareCallback[A, B]) {
	// Check for removed or changed keys
	for key, value := range b {
		if oldValue, ok := a[key]; ok {
			if !compareFunction(key, oldValue, value) {
				onChanged(key, oldValue, value)
			}
		} else {
			onRemoved(key, nil, value)
		}
	}

	// Check for added keys
	for key, value := range a {
		if _, ok := b[key]; !ok {
			onAdded(key, value, nil)
		}
	}
}
