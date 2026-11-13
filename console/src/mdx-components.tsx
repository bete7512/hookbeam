import type { MDXComponents } from "mdx/types";

import { CodeFile } from "@/components/docs/code-file";
import { DecisionList } from "@/components/docs/decisions";
import { ArchitectureExplorer, LifecyclePlayer } from "@/components/docs/diagrams/architecture";
import { DataModel } from "@/components/docs/diagrams/data-model";
import { ConceptMap, OutboxPath, TraceLinks } from "@/components/docs/diagrams/small";
import { DeliveryStates } from "@/components/docs/diagrams/states";
import { Callout, Lead, NextPages, proseComponents, Source, Sources } from "@/components/docs/prose";

// The documentation's MDX pages see these: the prose elements and the components they place by name.
const docs: MDXComponents = {
  ...proseComponents,
  Lead,
  Callout,
  Source,
  Sources,
  NextPages,
  CodeFile,
  DecisionList,
  ArchitectureExplorer,
  LifecyclePlayer,
  DeliveryStates,
  DataModel,
  ConceptMap,
  TraceLinks,
  OutboxPath,
};

export function useMDXComponents(components: MDXComponents): MDXComponents {
  return { ...components, ...docs };
}
