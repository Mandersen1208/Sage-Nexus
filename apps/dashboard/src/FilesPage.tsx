import { useCallback, useEffect, useMemo, useState } from "react";
import { downloadFile, listWorkspaceFiles, type WorkspaceFile } from "./api.js";

const ROOT_PATH = ".";

function FilesPage() {
  const [files, setFiles] = useState<WorkspaceFile[]>([]);
  const [currentPath, setCurrentPath] = useState<string>(ROOT_PATH);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadFiles = useCallback(async (path: string) => {
    setLoading(true);
    setError(null);
    try {
      const result = await listWorkspaceFiles(path === ROOT_PATH ? "" : path);
      setFiles(sortFiles(result.files ?? []));
      setCurrentPath(result.path || ROOT_PATH);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workspace files");
      setFiles([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadFiles("");
  }, [loadFiles]);

  const breadcrumbs = useMemo(() => buildBreadcrumbs(currentPath), [currentPath]);

  const handleDownload = async (file: WorkspaceFile) => {
    try {
      setError(null);
      await downloadFile(file.path);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to download file");
    }
  };

  const copyPath = async (path: string) => {
    try {
      await navigator.clipboard.writeText(path);
    } catch {
      setError("Could not copy path to clipboard");
    }
  };

  return (
    <main className="files-page">
      <section className="files-head">
        <div>
          <p className="files-kicker">Workspace</p>
          <h1>Files</h1>
          <p className="files-subtitle">
            Browse generated docs, artifacts, and workspace output without leaving Sage Nexus.
          </p>
        </div>
        <button className="files-refresh" onClick={() => void loadFiles(currentPath)} disabled={loading}>
          Refresh
        </button>
      </section>

      <nav className="files-breadcrumbs" aria-label="Workspace path">
        {breadcrumbs.map((crumb, index) => (
          <button
            key={`${crumb.path}-${index}`}
            className="files-crumb"
            onClick={() => void loadFiles(crumb.path)}
            aria-current={index === breadcrumbs.length - 1 ? "page" : undefined}
          >
            {crumb.label}
          </button>
        ))}
      </nav>

      {error ? (
        <div className="files-alert" role="alert">
          {error}
        </div>
      ) : null}

      <section className="files-panel">
        <div className="files-panel-head">
          <div>
            <span className="files-panel-label">Current path</span>
            <strong>{currentPath === ROOT_PATH ? "Root" : currentPath}</strong>
          </div>
          <span className="files-count">{files.length} items</span>
        </div>

        {loading ? (
          <div className="files-state">Loading workspace files...</div>
        ) : files.length === 0 ? (
          <div className="files-state">No files found in this workspace path.</div>
        ) : (
          <div className="files-table-wrap">
            <table className="files-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Size</th>
                  <th>Modified</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {files.map((file) => (
                  <tr key={file.path}>
                    <td data-label="Name">
                      <button
                        className={`files-name ${file.isDir ? "is-folder" : "is-file"}`}
                        onClick={() => (file.isDir ? void loadFiles(file.path) : void copyPath(file.path))}
                      >
                        <span className="files-icon" aria-hidden="true" />
                        <span>
                          <strong>{file.name}</strong>
                          <small>{file.path}</small>
                        </span>
                      </button>
                    </td>
                    <td data-label="Size">{file.isDir ? "-" : formatFileSize(file.size)}</td>
                    <td data-label="Modified">{formatDate(file.modTime)}</td>
                    <td data-label="Actions">
                      <div className="files-actions">
                        <button className="files-action" onClick={() => void copyPath(file.path)}>
                          Copy path
                        </button>
                        {!file.isDir ? (
                          <button className="files-action primary" onClick={() => void handleDownload(file)}>
                            Download
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </main>
  );
}

function buildBreadcrumbs(path: string): Array<{ label: string; path: string }> {
  if (!path || path === ROOT_PATH) {
    return [{ label: "Root", path: ROOT_PATH }];
  }
  const parts = path.split("/").filter(Boolean);
  return [
    { label: "Root", path: ROOT_PATH },
    ...parts.map((part, index) => ({
      label: part,
      path: parts.slice(0, index + 1).join("/"),
    })),
  ];
}

function sortFiles(files: WorkspaceFile[]) {
  return [...files].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
}

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, index);
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`;
}

function formatDate(timestamp: number): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return "-";
  return new Date(timestamp * 1000).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export default FilesPage;
