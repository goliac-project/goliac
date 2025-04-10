package engine

import (
	"context"
	"sync"

	"github.com/goliac-project/goliac/internal/observability"
)

type Resource interface {
	*GithubTeamRepo | *GithubEnvironment | *GithubVariable | map[string]*GithubVariable
}

// concurrentCall is a helper function to load resources concurrently
// Returns a map[repository name][resource name] resource
func concurrentCall[R Resource](ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository, resourceName string, loadResource func(ctx context.Context, repository *GithubRepository) (map[string]R, error), feedback observability.RemoteObservability) (map[string]map[string]R, error) {

	resourcePerRepo := make(map[string]map[string]R)

	var wg sync.WaitGroup
	var wg2 sync.WaitGroup

	// Create buffered channels
	reposChan := make(chan *GithubRepository, len(repositories))
	errChan := make(chan error, 1) // will hold the first error
	resourceReposChan := make(chan struct {
		repoName string
		repos    map[string]R
	}, len(repositories))

	if maxGoroutines < 1 {
		maxGoroutines = 1
	}
	// Create worker goroutines
	for i := int64(0); i < maxGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range reposChan {
				repos, err := loadResource(ctx, repo)
				if err != nil {
					// Try to report the error
					select {
					case errChan <- err:
					default:
					}
					return
				}
				resourceReposChan <- struct {
					repoName string
					repos    map[string]R
				}{repo.Name, repos}
			}
		}()
	}

	// Send repositories to reposChan
	for _, repo := range repositories {
		reposChan <- repo
	}
	close(reposChan)

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for r := range resourceReposChan {
			if feedback != nil {
				feedback.LoadingAsset(resourceName, 1)
			}
			resourcePerRepo[r.repoName] = r.repos
		}
	}()

	// Wait for all goroutines to finish
	wg.Wait()
	close(resourceReposChan)
	wg2.Wait()

	// Check if any goroutine returned an error
	select {
	case err := <-errChan:
		return resourcePerRepo, err
	default:
		//nop
	}

	return resourcePerRepo, nil
}
