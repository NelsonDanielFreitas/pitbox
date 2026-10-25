export function PageHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="mb-6">
      <h1 className="text-3xl font-bold tracking-tight text-fg">{title}</h1>
      <p className="mt-2 max-w-2xl text-muted">{subtitle}</p>
    </div>
  );
}
