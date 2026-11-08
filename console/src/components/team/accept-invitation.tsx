"use client";

import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { acceptInvitation, switchWorkspace } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { useMe } from "@/api/queries";
import { FormError } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState } from "@/components/states";

// AcceptInvitation lands from the emailed link. A signed-in person joins with one click and is switched into the
// workspace; a signed-out person signs in or creates an account first and comes back here.
export function AcceptInvitation() {
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  const me = useMe();
  const here = `/invitations/accept?token=${encodeURIComponent(token)}`;
  const accept = useMutation({
    mutationFn: async () => {
      const res = await acceptInvitation(token);
      await switchWorkspace(res.member.org_id);
    },
    onSuccess: () => window.location.assign("/"),
  });
  if (!token) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Invitation</CardTitle>
          <CardDescription>This link carries no invitation token. Ask for a new invitation.</CardDescription>
        </CardHeader>
      </Card>
    );
  }
  if (me.isPending) return <LoadingState />;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Join the workspace</CardTitle>
        <CardDescription>{me.data ? "You are signed in; accepting adds the workspace to your account and switches you into it." : "Sign in, or create an account with the address the invitation was sent to; you come back here to accept."}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {me.data ? (
          <>
            <FormError message={accept.isError ? asApiError(accept.error).message : null} />
            <Button onClick={() => accept.mutate()} disabled={accept.isPending} data-testid="accept-invitation">
              Accept invitation
            </Button>
          </>
        ) : (
          <div className="flex flex-wrap gap-3">
            <Button asChild>
              <Link href={`/signup?next=${encodeURIComponent(here)}`} data-testid="accept-signup">
                Create an account
              </Link>
            </Button>
            <Button asChild variant="outline">
              <Link href={`/login?next=${encodeURIComponent(here)}`} data-testid="accept-login">
                Sign in
              </Link>
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
