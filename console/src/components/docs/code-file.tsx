import { readFileSync } from "node:fs";
import { join } from "node:path";

import { CodeBlock } from "@/components/docs/code-block";

// CodeFile shows a console file as it is, read when the page is built, so an example on a page is the same file
// its test runs. Server only: it reads the file system.
export function CodeFile({ path, language }: { path: string; language: string }) {
  const text = readFileSync(join(process.cwd(), path), "utf8").trimEnd();
  return (
    <CodeBlock>
      <code className={`language-${language}`}>{text}</code>
    </CodeBlock>
  );
}
