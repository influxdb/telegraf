package github

import (
	"reflect"
	"testing"

	gh "github.com/google/go-github/github"
	"github.com/stretchr/testify/require"
)

func TestSplitRepositoryNameWithWorkingExample(t *testing.T) {
	owner, repository, _ := splitRepositoryName("influxdata/influxdb")

	require.Equal(t, "influxdata", owner)
	require.Equal(t, "influxdb", repository)
}

func TestSplitRepositoryNameWithNoSlash(t *testing.T) {
	_, _, error := splitRepositoryName("influxdata-influxdb")

	require.NotNil(t, error)
}

func TestGetLicenseWhenExists(t *testing.T) {
	licenseName := "MIT"
	license := gh.License{Name: &licenseName}
	repository := gh.Repository{License: &license}

	getLicenseReturn := getLicense(&repository)

	require.Equal(t, "MIT", getLicenseReturn)
}

func TestGetLicenseWhenMissing(t *testing.T) {
	repository := gh.Repository{}

	getLicenseReturn := getLicense(&repository)

	require.Equal(t, "None", getLicenseReturn)
}

func TestGetTags(t *testing.T) {
	licenseName := "MIT"
	license := gh.License{Name: &licenseName}

	ownerName := "influxdata"
	owner := gh.User{Login: &ownerName}

	fullName := "influxdata/influxdb"
	repositoryName := "influxdb"

	language := "Go"

	repository := gh.Repository{FullName: &fullName, Name: &repositoryName, License: &license, Owner: &owner, Language: &language}

	getTagsReturn := getTags(&repository)

	correctTagsReturn := map[string]string{
		"full_name": fullName,
		"owner":     ownerName,
		"name":      repositoryName,
		"language":  language,
		"license":   licenseName,
	}

	require.True(t, reflect.DeepEqual(getTagsReturn, correctTagsReturn))
}

func TestGetFields(t *testing.T) {
	stars := 1
	forks := 2
	open_issues := 3
	size := 4

	repository := gh.Repository{StargazersCount: &stars, ForksCount: &forks, OpenIssuesCount: &open_issues, Size: &size}

	getFieldsReturn := getFields(&repository)

	correctFieldReturn := make(map[string]interface{})

	correctFieldReturn["stars"] = 1
	correctFieldReturn["forks"] = 2
	correctFieldReturn["open_issues"] = 3
	correctFieldReturn["size"] = 4

	require.True(t, reflect.DeepEqual(getFieldsReturn, correctFieldReturn))
}
