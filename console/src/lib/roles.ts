// The capability matrix the API enforces (services.Authorizer); the console only uses it to hide or disable
// controls with a reason, never as the real check.
export type Role = "owner" | "admin" | "developer" | "viewer";

export type Capability = "readMetadata" | "readPayloads" | "replay" | "manageEndpoints" | "manageKeys" | "manageMembers" | "manageOwners";

const matrix: Record<Capability, Role[]> = {
  readMetadata: ["owner", "admin", "developer", "viewer"],
  readPayloads: ["owner", "admin", "developer"],
  replay: ["owner", "admin", "developer"],
  manageEndpoints: ["owner", "admin"],
  manageKeys: ["owner", "admin"],
  manageMembers: ["owner", "admin"],
  manageOwners: ["owner"],
};

export const roles: Role[] = ["owner", "admin", "developer", "viewer"];

// goauthRole is the role goauth stores for a Hookbeam role; developer and viewer are both goauth members.
export function goauthRole(role: Role): "owner" | "admin" | "member" {
  return role === "owner" || role === "admin" ? role : "member";
}

export function can(role: string | null | undefined, capability: Capability): boolean {
  return role ? matrix[capability].includes(role as Role) : false;
}

// deny explains a refusal the way the API would.
export function deny(role: string | null | undefined, capability: Capability): string {
  const what: Record<Capability, string> = {
    readMetadata: "read",
    readPayloads: "read payloads and response previews",
    replay: "replay or cancel deliveries",
    manageEndpoints: "manage endpoints",
    manageKeys: "manage keys",
    manageMembers: "manage members",
    manageOwners: "manage owners",
  };
  return `a ${role ?? "guest"} cannot ${what[capability]}`;
}
