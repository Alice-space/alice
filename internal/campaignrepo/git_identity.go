package campaignrepo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type gitIdentityScope string

const (
	gitIdentityScopeLocal  gitIdentityScope = "local"
	gitIdentityScopeGlobal gitIdentityScope = "global"
)

type gitIdentity struct {
	Name  string
	Email string
	Scope gitIdentityScope
}

type gitIdentityErrorKind string

const (
	gitIdentityErrorMissing    gitIdentityErrorKind = "missing"
	gitIdentityErrorIncomplete gitIdentityErrorKind = "incomplete"
)

type gitIdentityError struct {
	Path          string
	Scope         gitIdentityScope
	Kind          gitIdentityErrorKind
	MissingFields []string
}

func (e *gitIdentityError) Error() string {
	if e == nil {
		return "git identity required"
	}

	path := strings.TrimSpace(e.Path)
	if path == "" {
		path = "repo"
	}

	switch e.Kind {
	case gitIdentityErrorIncomplete:
		scope := strings.TrimSpace(string(e.Scope))
		if scope == "" {
			scope = "configured"
		}
		missing := append([]string(nil), e.MissingFields...)
		slices.Sort(missing)
		return fmt.Sprintf(
			"git identity incomplete in %s config for %s: missing %s; set both user.name and user.email in that scope or clear the partial config",
			scope,
			path,
			strings.Join(missing, ", "),
		)
	default:
		return fmt.Sprintf(
			"git identity required for %s: set repo local user.name/user.email or global user.name/user.email before Alice commits or merges",
			path,
		)
	}
}

func gitIdentityConfigArgs(path string) ([]string, error) {
	identity, err := resolveGitIdentity(path)
	if err != nil {
		return nil, err
	}
	return []string{
		"-c", "user.name=" + identity.Name,
		"-c", "user.email=" + identity.Email,
	}, nil
}

func resolveGitIdentity(path string) (gitIdentity, error) {
	path = strings.TrimSpace(path)
	for _, scope := range []gitIdentityScope{gitIdentityScopeLocal, gitIdentityScopeGlobal} {
		name, email, err := gitIdentityAtScope(path, scope)
		if err != nil {
			return gitIdentity{}, err
		}
		if name == "" && email == "" {
			continue
		}
		if name == "" || email == "" {
			missing := make([]string, 0, 2)
			if name == "" {
				missing = append(missing, "user.name")
			}
			if email == "" {
				missing = append(missing, "user.email")
			}
			return gitIdentity{}, &gitIdentityError{
				Path:          path,
				Scope:         scope,
				Kind:          gitIdentityErrorIncomplete,
				MissingFields: missing,
			}
		}
		return gitIdentity{
			Name:  name,
			Email: email,
			Scope: scope,
		}, nil
	}

	return gitIdentity{}, &gitIdentityError{
		Path: path,
		Kind: gitIdentityErrorMissing,
	}
}

func gitIdentityAtScope(path string, scope gitIdentityScope) (string, string, error) {
	name, err := gitConfigValue(path, scope, "user.name")
	if err != nil {
		return "", "", err
	}
	email, err := gitConfigValue(path, scope, "user.email")
	if err != nil {
		return "", "", err
	}
	return name, email, nil
}

func gitConfigValue(path string, scope gitIdentityScope, key string) (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	args := []string{"-C", path, "config", "--get"}
	switch scope {
	case gitIdentityScopeLocal:
		args = append(args, "--local")
	case gitIdentityScopeGlobal:
		args = append(args, "--global")
	}
	args = append(args, key)
	cmd := exec.Command(bin, args...)
	cmd.Env = isolatedGitEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("git config %s %s failed: %w: %s", scope, key, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func LooksLikeGitIdentityProblem(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "git identity") ||
		strings.Contains(normalized, "author identity unknown") ||
		strings.Contains(normalized, "committer identity unknown") ||
		strings.Contains(normalized, "unable to auto-detect email address")
}

func blockedReasonSeverity(reason string) string {
	if LooksLikeGitIdentityProblem(reason) {
		return "error"
	}
	return "warning"
}
