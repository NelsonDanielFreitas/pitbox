import { Card } from "./Card";

export function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <Card className="border-dashed text-center">
      <h2 className="text-lg font-semibold text-pitbox-ink">{title}</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-pitbox-muted">{body}</p>
    </Card>
  );
}
