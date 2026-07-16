package policy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthorizationChecksStayInPolicyLayer(t *testing.T) {
	t.Parallel()

	internalRoot := ".."
	policyRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve policy root: %v", err)
	}

	banned := []string{
		"permissionsForRole(",
		"hasPermission(",
		"permissionClassCreate",
		"permissionClassView",
		"permissionSessionJoin",
		"permissionMediaPublish",
	}
	for _, permission := range permissionOrder {
		banned = append(banned, `"`+string(permission)+`"`)
	}
	for _, role := range []string{
		string(OrganizationRoleAdmin),
		string(OrganizationRoleTeacher),
		string(OrganizationRoleStudent),
		string(OrganizationRoleGuest),
		string(ClassRoleOwner),
		string(ClassRoleCoTeacher),
		string(ClassRoleTeachingAssistant),
	} {
		banned = append(banned, `"`+role+`"`)
	}

	err = filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			absolutePath, resolveErr := filepath.Abs(path)
			if resolveErr != nil {
				return resolveErr
			}
			if filepath.Clean(absolutePath) == filepath.Clean(policyRoot) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, pattern := range banned {
			if strings.Contains(string(contents), pattern) {
				t.Errorf("authorization literal or helper %q must not live outside policy layer: %s", pattern, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal policy boundaries: %v", err)
	}
}
