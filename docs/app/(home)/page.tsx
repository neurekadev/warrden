import Link from 'next/link';

export default function HomePage() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col items-center justify-center px-6 py-20 text-center">
      <img src="/logo.png" alt="wArrden shield" className="mb-8 h-36 w-36 object-contain" />
      <p className="mb-3 text-sm font-medium uppercase tracking-[0.24em] text-fd-muted-foreground">
        Automated arr maintenance
      </p>
      <h1 className="text-balance text-4xl font-semibold tracking-tight sm:text-6xl">Keep your media libraries moving</h1>
      <p className="mt-6 max-w-2xl text-pretty text-lg text-fd-muted-foreground">
        wArrden schedules missing and upgrade searches, then clears queue entries that are stuck on known import failures.
      </p>
      <div className="mt-10 flex flex-wrap justify-center gap-3">
        <Link
          href="/docs/quickstart"
          className="rounded-lg bg-fd-primary px-5 py-3 font-medium text-fd-primary-foreground transition-opacity hover:opacity-90"
        >
          Get started
        </Link>
        <Link
          href="/docs"
          className="rounded-lg border border-fd-border px-5 py-3 font-medium text-fd-foreground transition-colors hover:bg-fd-accent"
        >
          Read the docs
        </Link>
      </div>
    </main>
  );
}
