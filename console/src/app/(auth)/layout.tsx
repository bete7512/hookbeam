import Link from "next/link";

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto flex min-h-screen w-full max-w-md flex-col justify-center gap-6 px-6 py-12">
      <Link href="/" className="text-lg font-semibold">
        Hookbeam
      </Link>
      {children}
    </main>
  );
}
