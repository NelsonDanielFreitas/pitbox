import { Link } from "@tanstack/react-router";
import { PitboxSecure } from "pitbox-secure";
import { useEffect, useRef, useState } from "react";

import { hashUserId } from "@/db/client";
import { Button } from "@/shared/ui/Button";

import { leaveTestDatabase, runDiagnostics, type CheckResult } from "./checks";

const MARK = { pass: "OK", warn: "AVISO", fail: "FALHOU" } as const;

/**
 * Debug-only (VITE_DIAGNOSTICS=1, see the route). Not translated on purpose: it is a tool for
 * the developer, never shown in a release build.
 */
export default function DiagnosticsPage() {
  const [results, setResults] = useState<CheckResult[]>([]);
  const [running, setRunning] = useState(false);
  const [biometric, setBiometric] = useState<string>("");

  const started = useRef(false);
  useEffect(() => {
    // Runs once on open (the screen has no keyboard to ask); the button repeats it.
    if (started.current) return;
    started.current = true;
    void start();
  }, []);

  async function start() {
    setRunning(true);
    setResults([]);
    try {
      // Loaded here, not at the top: the page only needs the native engine once it runs.
      const { createNativeDatabase } = await import("@/db/native-database");
      await runDiagnostics(
        { plugin: PitboxSecure, database: await createNativeDatabase(), hashUserId },
        (result) => {
          console.warn(`[diag] ${MARK[result.status]} ${result.name} — ${result.detail}`);
          setResults((previous) => [...previous, result]);
        },
      );
      console.warn("[diag] done");
    } catch (error) {
      const text = error instanceof Error ? `${error.name}: ${error.message}` : "unknown error";
      console.warn(`[diag] FALHOU diagnostics — ${text}`);
      setResults((previous) => [...previous, { name: "Diagnóstico", status: "fail", detail: text }]);
    } finally {
      setRunning(false);
    }
  }

  async function leaveDatabase() {
    setRunning(true);
    try {
      const { createNativeDatabase } = await import("@/db/native-database");
      const result = await leaveTestDatabase(await createNativeDatabase());
      console.warn(`[diag] ${MARK[result.status]} ${result.name} — ${result.detail}`);
      setResults((previous) => [...previous, result]);
    } finally {
      setRunning(false);
    }
  }

  async function askBiometric() {
    let text: string;
    try {
      await PitboxSecure.authenticate({ title: "PitBox", subtitle: "Teste de diagnóstico" });
      text = "OK: autenticado";
    } catch (error) {
      text = `${error instanceof Error ? error.message : "erro"} (cancelar também aparece aqui)`;
    }
    console.warn(`[diag] biometric — ${text}`);
    setBiometric(text);
  }

  return (
    <main className="mx-auto max-w-xl space-y-4 px-4 py-6">
      <h1 className="text-2xl font-bold">Diagnóstico nativo (debug)</h1>
      <p className="text-sm text-muted">
        Usa um utilizador de teste: não toca nos teus dados nem nas tuas chaves.
      </p>
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => void start()} disabled={running}>
          {running ? "A correr…" : "Correr diagnóstico"}
        </Button>
        <Button onClick={() => void askBiometric()}>Testar biometria</Button>
        <Button onClick={() => void leaveDatabase()} disabled={running}>
          Deixar base de teste (adb)
        </Button>
      </div>
      {biometric ? <p className="text-sm">Biometria: {biometric}</p> : null}
      <ul className="space-y-2">
        {results.map((result) => (
          <li key={result.name} className="rounded border border-border p-3">
            <strong>
              {MARK[result.status]} · {result.name}
            </strong>
            <div className="text-sm text-muted">{result.detail}</div>
          </li>
        ))}
      </ul>
      <Link to="/teams" className="text-sm underline">
        Ir para a app
      </Link>
    </main>
  );
}
