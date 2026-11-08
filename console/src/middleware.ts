import { NextResponse, type NextRequest } from "next/server";

// The access cookie goauth sets at login. The middleware only checks that it is present; the API validates it.
const accessCookie = "goauth_access_hookbeam";

// Signed-in pages redirect to /login without a session cookie; auth pages send a signed-in person home.
export function middleware(req: NextRequest) {
  const signedIn = req.cookies.has(accessCookie);
  const { pathname } = req.nextUrl;
  const authPage = pathname === "/login" || pathname === "/signup";
  if (!signedIn && !authPage) {
    const url = req.nextUrl.clone();
    url.pathname = "/login";
    url.search = `?next=${encodeURIComponent(pathname)}`;
    return NextResponse.redirect(url);
  }
  if (signedIn && authPage) {
    const url = req.nextUrl.clone();
    url.pathname = "/";
    url.search = "";
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/", "/account/:path*", "/login", "/signup"],
};
