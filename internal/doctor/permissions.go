// Package doctor provides local, non-secret diagnostic checks.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Finding struct {
	Severity Severity
	Code     string
	Subject  string
	Message  string
}

type Paths struct {
	TokenFile      string
	EnvFile        string
	ConfigFile     string
	CacheDirectory string
}

// CheckPermissions inspects explicit configuration, credential, and cache
// paths without opening or modifying them. Messages intentionally omit paths.
func CheckPermissions(paths Paths) []Finding {
	var findings []Finding
	if paths.TokenFile != "" {
		findings = append(findings, checkFile("token_file", paths.TokenFile, true)...)
	}
	if paths.EnvFile != "" {
		findings = append(findings, checkFile("dotenv_file", paths.EnvFile, true)...)
	}
	if paths.ConfigFile != "" {
		findings = append(findings, checkFile("config_file", paths.ConfigFile, false)...)
	}
	if paths.CacheDirectory != "" {
		findings = append(findings, checkCache(paths.CacheDirectory)...)
	}
	return findings
}

func checkFile(subject, path string, secret bool) []Finding {
	info, err := os.Lstat(path)
	if err != nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "path_unusable",
			Subject:  subject,
			Message:  "selected file cannot be inspected",
		}}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []Finding{{
			Severity: SeverityError,
			Code:     "symlink_rejected",
			Subject:  subject,
			Message:  "selected file is a symlink",
		}}
	}
	if !info.Mode().IsRegular() {
		return []Finding{{
			Severity: SeverityError,
			Code:     "not_regular",
			Subject:  subject,
			Message:  "selected path is not a regular file",
		}}
	}
	if runtime.GOOS == "windows" {
		return nil
	}

	var findings []Finding
	if info.Mode().Perm()&0o022 != 0 {
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Code:     "group_or_other_writable",
			Subject:  subject,
			Message:  "selected file is writable by group or others",
		})
	}
	if secret && info.Mode().Perm()&0o044 != 0 {
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Code:     "secret_group_or_other_readable",
			Subject:  subject,
			Message:  "secret-bearing file is readable by group or others",
		})
	}
	return findings
}

func checkCache(directory string) []Finding {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "cache_unusable",
			Subject:  "cache_directory",
			Message:  "cache directory cannot be inspected",
		}}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []Finding{{
			Severity: SeverityError,
			Code:     "symlink_rejected",
			Subject:  "cache_directory",
			Message:  "cache directory is a symlink",
		}}
	}
	if !info.IsDir() {
		return []Finding{{
			Severity: SeverityError,
			Code:     "not_directory",
			Subject:  "cache_directory",
			Message:  "cache path is not a directory",
		}}
	}

	var findings []Finding
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o077 != 0 {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Code:     "cache_directory_exposed",
				Subject:  "cache_directory",
				Message:  "cache directory is accessible by group or others",
			})
		}
		if info.Mode().Perm()&0o200 == 0 {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Code:     "cache_directory_not_writable",
				Subject:  "cache_directory",
				Message:  "cache directory is not owner-writable",
			})
		}
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := filepath.Join(directory, "cache.db"+suffix)
		databaseInfo, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		subject := "cache_database"
		if suffix != "" {
			subject = fmt.Sprintf("cache_database%s", suffix)
		}
		if err != nil {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Code:     "cache_file_unusable",
				Subject:  subject,
				Message:  "cache file cannot be inspected",
			})
			continue
		}
		if databaseInfo.Mode()&os.ModeSymlink != 0 {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Code:     "symlink_rejected",
				Subject:  subject,
				Message:  "cache file is a symlink",
			})
			continue
		}
		if !databaseInfo.Mode().IsRegular() {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Code:     "not_regular",
				Subject:  subject,
				Message:  "cache path is not a regular file",
			})
			continue
		}
		if runtime.GOOS != "windows" && databaseInfo.Mode().Perm()&0o077 != 0 {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Code:     "cache_file_exposed",
				Subject:  subject,
				Message:  "cache file is accessible by group or others",
			})
		}
	}
	return findings
}
