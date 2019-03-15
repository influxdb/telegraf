package github

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/github"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
	"golang.org/x/oauth2"
)

// GitHub - plugin main structure
type GitHub struct {
	Repositories []string          `toml:"repositories"`
	APIKey       string            `toml:"api_key"`
	HTTPTimeout  internal.Duration `toml:"http_timeout"`
	githubClient *gh.Client
}

const sampleConfig = `
  ## Specify a list of repositories
  repositories = ["influxdata/influxdb"]

  ## API Key for GitHub API requests
  api_key = ""

  ## Timeout for GitHub API requests
  http_timeout = "5s"
`

// NewGitHub returns a new instance of the GitHub input plugin
func NewGitHub() *GitHub {
	return &GitHub{
		HTTPTimeout: internal.Duration{Duration: time.Second * 5},
	}
}

// SampleConfig returns sample configuration for this plugin.
func (github *GitHub) SampleConfig() string {
	return sampleConfig
}

// Description returns the plugin description.
func (github *GitHub) Description() string {
	return "Read repository information from GitHub, including forks, stars, and more."
}

// Create HTTP Client
func (github *GitHub) createGitHubClient() (*gh.Client, error) {
	var githubClient *gh.Client

	if github.APIKey != "" {
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: github.APIKey},
		)
		tc := oauth2.NewClient(ctx, ts)

		githubClient = gh.NewClient(tc)
	} else {
		githubClient = gh.NewClient(nil)
	}

	return githubClient, nil
}

// Gather GitHub Metrics
func (github *GitHub) Gather(acc telegraf.Accumulator) error {
	if github.githubClient == nil {
		githubClient, err := github.createGitHubClient()

		if err != nil {
			return err
		}

		github.githubClient = githubClient
	}

	var wg sync.WaitGroup
	wg.Add(len(github.Repositories))

	for _, repository := range github.Repositories {
		go func(s string, acc telegraf.Accumulator) {
			defer wg.Done()

			ctx := context.Background()

			splits := strings.Split(s, "/")

			if len(splits) != 2 {
				log.Printf("E! [github]: Error in plugin: %v is not of format 'owner/repository'", s)
				return
			}

			repository, response, err := github.githubClient.Repositories.Get(ctx, splits[0], splits[1])

			if _, ok := err.(*gh.RateLimitError); ok {
				log.Printf("E! [github]: %v of %v requests remaining", response.Rate.Remaining, response.Rate.Limit)
				return
			}

			fields := make(map[string]interface{})

			license := "None"

			if repository.GetLicense() != nil {
				license = *repository.License.Name
			}

			tags := map[string]string{
				"full_name": *repository.FullName,
				"owner":     *repository.Owner.Login,
				"name":      *repository.Name,
				"language":  *repository.Language,
				"license":   license,
			}

			fields["stars"] = repository.StargazersCount
			fields["forks"] = repository.ForksCount
			fields["open_issues"] = repository.OpenIssuesCount
			fields["size"] = repository.Size

			now := time.Now()

			acc.AddFields("github_repository", fields, tags, now)

			rateFields := make(map[string]interface{})
			rateTags := map[string]string{}

			rateFields["limit"] = response.Rate.Limit
			rateFields["remaining"] = response.Rate.Remaining

			acc.AddFields("github_rate_limit", rateFields, rateTags, now)
		}(repository, acc)
	}

	wg.Wait()
	return nil
}

func init() {
	inputs.Add("github", func() telegraf.Input {
		return &GitHub{}
	})
}
