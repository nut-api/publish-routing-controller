package github

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type GithubClient struct {
	RepoURL     string
	GithubToken string
}

func CloneOrPullRepo(ctx context.Context, githubClient GithubClient) error {
	// Clone or pull the GitHub repo to ensure we have the latest version
	// Using go-git library: https://pkg.go.dev/github.com/go-git/go-git/v6
	l := logf.FromContext(ctx)

	auth := &http.BasicAuth{
		Username: "abc123", // yes, this can be anything except an empty string
		Password: githubClient.GithubToken,
	}

	// Check if ./repo exists, if not clone, if yes pull
	if _, err := os.Stat("app-repo"); os.IsNotExist(err) {
		l.Info("Cloning GitHub repo")
		_, err := git.PlainClone("app-repo", &git.CloneOptions{
			URL:  githubClient.RepoURL,
			Auth: auth,
			Tags: git.NoTags,
			// Progress: os.Stdout,
		})
		if err != nil {
			return err
		}
	} else {
		l.Info("Repo exists, pulling latest changes")
	}
	repo, err := git.PlainOpen("app-repo")
	if err != nil {
		return fmt.Errorf("failed to open git repo")
	}
	// Get the working directory for the repository
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	// Pull the latest changes from the origin remote and merge into the current branch
	err = w.Pull(&git.PullOptions{RemoteName: "origin",
		Auth:  auth,
		Force: true,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			l.Info("Repo already up-to-date")
			return nil
		}
		return err
	}
	l.Info("Successfully pulled latest changes from GitHub repo")
	return nil
}

func CommitAndPushChanges(ctx context.Context, githubClient GithubClient, commitMsg string) error {
	// Change change on repo
	l := logf.FromContext(ctx)
	repo, err := git.PlainOpen("app-repo")
	if err != nil {
		return fmt.Errorf("failed to open git repo")
	}
	// Get the working directory for the repository
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	// Check if there are any changes
	status, err := w.Status()
	if err != nil {
		return err
	}
	if status.IsClean() {
		l.Info("No changes to commit")
		return nil
	}

	// Add changes to staging area
	err = w.AddWithOptions(&git.AddOptions{
		All: true,
	})
	if err != nil {
		return err
	}

	// Commit the changes
	commit, err := w.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Publish Routing Controller",
			Email: "nut-api@apiplus.tech",
			When:  time.Now(),
		},
	})
	if err != nil {
		return err
	}

	// Push the changes to the remote repository
	auth := &http.BasicAuth{
		Username: "abc123", // yes, this can be anything except an empty string
		Password: githubClient.GithubToken,
	}
	err = repo.Push(&git.PushOptions{
		Auth: auth,
	})
	if err != nil {
		return err
	}

	l.Info("Committed and pushed changes to GitHub", "commit", commit.String())
	return nil

}
