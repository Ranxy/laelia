package workspace

import (
	"os"
	"path/filepath"
	"time"
)

// Summary is one per-agent workspace directory on a machine.
type Summary struct {
	DirectoryName  string
	TotalSizeBytes int64
	LastModified   time.Time
	FileCount      int64
}

type workspaceSummary struct {
	totalSizeBytes int64
	fileCount      int64
	latestMtime    time.Time
}

func summarizeWorkspaceDirectory(dirPath string) workspaceSummary {
	var summary workspaceSummary
	rootInfo, err := os.Stat(dirPath)
	if err != nil {
		return summary
	}
	summary.latestMtime = rootInfo.ModTime()
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return summary
	}
	for _, e := range entries {
		child := summarizeWorkspaceEntry(filepath.Join(dirPath, e.Name()), e)
		summary.totalSizeBytes += child.totalSizeBytes
		summary.fileCount += child.fileCount
		if child.latestMtime.After(summary.latestMtime) {
			summary.latestMtime = child.latestMtime
		}
	}
	return summary
}

func summarizeWorkspaceEntry(entryPath string, e os.DirEntry) workspaceSummary {
	info, err := os.Stat(entryPath)
	if err != nil {
		return workspaceSummary{}
	}
	if e.IsDir() {
		return summarizeWorkspaceDirectory(entryPath)
	}
	if e.Type().IsRegular() {
		return workspaceSummary{totalSizeBytes: info.Size(), fileCount: 1, latestMtime: info.ModTime()}
	}
	return workspaceSummary{latestMtime: info.ModTime()}
}

// Scan summarizes every directory directly under root (files and sockets such
// as daemon.sock are skipped). Individual failures are tolerated, mirroring
// raft.
func Scan(root string) ([]Summary, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		summary := summarizeWorkspaceDirectory(filepath.Join(root, e.Name()))
		out = append(out, Summary{
			DirectoryName:  e.Name(),
			TotalSizeBytes: summary.totalSizeBytes,
			LastModified:   summary.latestMtime,
			FileCount:      summary.fileCount,
		})
	}
	return out, nil
}
