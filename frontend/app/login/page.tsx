"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";
import { api } from "@/lib/api";
import { Spinner } from "@/components/ui";

export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/auth/login", { username, password });
      router.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-wrap">
      <form className="card login-card" onSubmit={submit}>
        <div className="card-body stack">
          <div className="brand" style={{ padding: 0 }}>
            <div className="brand-mark">T</div>
            <div>
              Talos Controller
              <small>Sign in to continue</small>
            </div>
          </div>
          <label className="field">
            Username
            <input type="text" value={username} autoComplete="username" onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label className="field">
            Password
            <input
              type="password"
              value={password}
              autoComplete="current-password"
              autoFocus
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          {error ? <div className="err-box">{error}</div> : null}
          <button className="btn btn-primary" style={{ width: "100%", justifyContent: "center" }} disabled={busy}>
            {busy ? <Spinner /> : null} Sign in
          </button>
        </div>
      </form>
    </div>
  );
}
