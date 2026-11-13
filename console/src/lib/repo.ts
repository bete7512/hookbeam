// The public repository: the landing page links it and the documentation links source files into it.
export const repoURL = "https://github.com/bete7512/hookbeam";

// sourceURL links a repository path; a path ending in "/" is a folder.
export function sourceURL(path: string): string {
  return `${repoURL}/${path.endsWith("/") ? "tree" : "blob"}/main/${path.replace(/\/$/, "")}`;
}
