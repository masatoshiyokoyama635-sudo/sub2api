//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data     string
	getCalls int
	setCalls int
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	s.getCalls++
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.setCalls++
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release            *GitHubRelease
	recentReleases     []*GitHubRelease
	recentErr          error
	latestFetchCalls   int
	recentFetchCalls   int
	downloadCalls      int
	checksumFetchCalls int
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	s.latestFetchCalls++
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	s.recentFetchCalls++
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	s.downloadCalls++
	return errors.New("unexpected download")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	s.checksumFetchCalls++
	return nil, errors.New("unexpected checksum fetch")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceOfficialBinaryLifecycleGuard(t *testing.T) {
	builds := []struct {
		name      string
		version   string
		buildType string
		allowed   bool
	}{
		{name: "official release", version: "0.1.162", buildType: "release", allowed: true},
		{name: "custom build type", version: "0.1.162", buildType: "custom"},
		{name: "source build", version: "0.1.162", buildType: "source"},
		{name: "empty build type", version: "0.1.162", buildType: ""},
		{name: "unknown build type", version: "0.1.162", buildType: "nightly"},
		{name: "official release with v prefix", version: "v0.1.162", buildType: "release", allowed: true},
		{name: "custom version with release build type", version: "0.1.162-zz", buildType: "release"},
		{name: "custom version with source build type", version: "0.1.162-zz", buildType: "source"},
		{name: "custom version with custom build type", version: "0.1.162-zz", buildType: "custom"},
		{name: "development version", version: "dev", buildType: "release"},
		{name: "custom suffix", version: "0.1.162-custom", buildType: "release"},
		{name: "dirty custom suffix", version: "0.1.162-zz+dirty", buildType: "release"},
		{name: "uppercase custom suffix", version: "0.1.162-ZZ", buildType: "release"},
		{name: "release candidate", version: "0.1.162-rc.1", buildType: "release"},
		{name: "build metadata", version: "0.1.162+official", buildType: "release"},
	}

	lifecycles := []struct {
		name string
		run  func(context.Context, *UpdateService) error
	}{
		{
			name: "perform update",
			run: func(ctx context.Context, svc *UpdateService) error {
				return svc.PerformUpdate(ctx)
			},
		},
		{
			name: "rollback backup",
			run: func(_ context.Context, svc *UpdateService) error {
				return svc.Rollback()
			},
		},
		{
			name: "list rollback versions",
			run: func(ctx context.Context, svc *UpdateService) error {
				_, err := svc.ListRollbackVersions(ctx)
				return err
			},
		},
		{
			name: "rollback to version",
			run: func(ctx context.Context, svc *UpdateService) error {
				return svc.RollbackToVersion(ctx, "0.1.160")
			},
		},
	}

	for _, build := range builds {
		for _, lifecycle := range lifecycles {
			t.Run(build.name+"/"+lifecycle.name, func(t *testing.T) {
				cache := &updateServiceCacheStub{}
				githubClient := &updateServiceGitHubClientStub{
					release:        &GitHubRelease{TagName: "v0.1.162", Name: "v0.1.162"},
					recentReleases: []*GitHubRelease{{TagName: "v0.1.160"}},
				}
				svc := NewUpdateService(cache, githubClient, build.version, build.buildType)

				err := lifecycle.run(context.Background(), svc)

				if build.allowed {
					require.NotErrorIs(t, err, ErrCustomBuildOnlineUpdateUnsupported)
					return
				}
				require.ErrorIs(t, err, ErrCustomBuildOnlineUpdateUnsupported)
				require.Zero(t, cache.getCalls, "guard must run before reading update cache")
				require.Zero(t, cache.setCalls, "guard must run before writing update cache")
				require.Zero(t, githubClient.latestFetchCalls, "guard must run before fetching the latest release")
				require.Zero(t, githubClient.recentFetchCalls, "guard must run before fetching rollback releases")
				require.Zero(t, githubClient.downloadCalls, "guard must run before downloading release files")
				require.Zero(t, githubClient.checksumFetchCalls, "guard must run before fetching checksums")
			})
		}
	}
}

func TestUpdateServiceCheckUpdateTreatsCustomSuffixAsSameVersion(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.156",
				Name:    "v0.1.156",
			},
		},
		"0.1.156-zz",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.1.156-zz", info.CurrentVersion)
	require.Equal(t, "0.1.156", info.LatestVersion)
}

func TestCompareVersionsHandlesSuffixes(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    int
	}{
		{name: "same with custom suffix", current: "0.1.156-zz", latest: "v0.1.156", want: 0},
		{name: "same when latest has custom suffix", current: "v0.1.156", latest: "0.1.156-zz", want: 0},
		{name: "same with build metadata", current: "0.1.156+custom", latest: "v0.1.156", want: 0},
		{name: "custom suffix older", current: "0.1.155-zz", latest: "v0.1.156", want: -1},
		{name: "custom suffix newer", current: "0.1.157-zz", latest: "v0.1.156", want: 1},
		{name: "release candidate remains older than release", current: "0.1.159-rc1", latest: "v0.1.159", want: -1},
		{name: "beta remains older than release", current: "v0.1.159-beta.1", latest: "0.1.159", want: -1},
		{name: "release remains newer than release candidate", current: "0.1.159", latest: "v0.1.159-rc1", want: 1},
		{name: "invalid current version sorts below valid release", current: "0.1.159garbage", latest: "v0.1.159", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compareVersions(tt.current, tt.latest))
		})
	}
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsRejectsCustomRuntimeVersion(t *testing.T) {
	githubClient := &updateServiceGitHubClientStub{
		recentReleases: []*GitHubRelease{{TagName: "v0.1.155"}},
	}
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		githubClient,
		"0.1.156-zz",
		"release",
	)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.ErrorIs(t, err, ErrCustomBuildOnlineUpdateUnsupported)
	require.Nil(t, versions)
	require.Zero(t, githubClient.recentFetchCalls)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
