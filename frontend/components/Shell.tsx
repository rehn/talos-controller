"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { ReactNode, useEffect, useState } from "react";
import { api } from "@/lib/api";

const ICONS: Record<string, ReactNode> = {
  dashboard: <path d="M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z" />,
  nodes: (
    <>
      <rect x="3" y="4" width="18" height="6" rx="1.5" />
      <rect x="3" y="14" width="18" height="6" rx="1.5" />
      <path d="M7 7h.01M7 17h.01" />
    </>
  ),
  workloads: <path d="M12 2 3 7v10l9 5 9-5V7l-9-5zm0 0v20M3 7l9 5 9-5" />,
  etcd: (
    <>
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6" />
    </>
  ),
  upgrades: <path d="M12 19V5m0 0-6 6m6-6 6 6" />,
  config: (
    <>
      <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9l-6-6z" />
      <path d="M14 3v6h6M9 14h6M9 18h4" />
    </>
  ),
  jobs: <path d="M4 6h16M4 12h10M4 18h7" />,
  drift: (
    <>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="M6 8.5V14a4 4 0 0 0 4 4h5.5M18 15.5V10a4 4 0 0 0-4-4H8.5" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
    </>
  ),
};

const NAV = [
  { href: "/", label: "Dashboard", icon: "dashboard" },
  { href: "/nodes/", label: "Nodes", icon: "nodes" },
  { href: "/workloads/", label: "Workloads", icon: "workloads" },
  { href: "/etcd/", label: "etcd", icon: "etcd" },
  { href: "/upgrades/", label: "Upgrades", icon: "upgrades" },
  { href: "/config/", label: "Config patches", icon: "config" },
  { href: "/drift/", label: "Config drift", icon: "drift" },
  { href: "/jobs/", label: "Jobs", icon: "jobs" },
  { href: "/settings/", label: "Settings", icon: "settings" },
];

function isActive(pathname: string, href: string) {
  const p = pathname.endsWith("/") ? pathname : pathname + "/";
  if (href === "/") return p === "/";
  if (href === "/nodes/") return p.startsWith("/nodes/") || p.startsWith("/node/");
  return p.startsWith(href);
}

export default function Shell({ children }: { children: ReactNode }) {
  const pathname = usePathname() || "/";
  const router = useRouter();
  const isLogin = pathname.startsWith("/login");
  const [user, setUser] = useState<string | null>(null);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    if (isLogin) {
      setChecked(true);
      return;
    }
    api
      .get<{ user: string }>("/auth/me")
      .then((r) => setUser(r.user))
      .catch(() => router.replace("/login/"))
      .finally(() => setChecked(true));
  }, [isLogin, router]);

  if (isLogin) return <>{children}</>;
  if (!checked || !user) return null;

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">T</div>
          <div>
            Talos Controller
            <small>cluster supervisor</small>
          </div>
        </div>
        <nav className="nav">
          {NAV.map((n) => (
            <Link key={n.href} href={n.href} className={isActive(pathname, n.href) ? "active" : ""}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                {ICONS[n.icon]}
              </svg>
              <span>{n.label}</span>
            </Link>
          ))}
        </nav>
        <div className="sidebar-footer">
          <span>{user}</span>
          <button
            className="btn btn-sm"
            onClick={async () => {
              await api.post("/auth/logout").catch(() => undefined);
              router.replace("/login/");
            }}
          >
            Log out
          </button>
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
