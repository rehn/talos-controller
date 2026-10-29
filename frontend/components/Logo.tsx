// Talos Controller logo: a "T" made of three connected cluster nodes.
// Keep in sync with app/icon.svg.
export default function Logo({ size = 28, title = "Talos Controller" }: { size?: number; title?: string }) {
  const bg = "#3b5bdb";
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label={title} style={{ flexShrink: 0, display: "block" }}>
      <rect width="64" height="64" rx="14" fill={bg} />
      <g stroke="#fff" strokeWidth="5" strokeLinecap="round">
        <line x1="16" y1="20" x2="48" y2="20" />
        <line x1="32" y1="20" x2="32" y2="46" />
      </g>
      <g fill="#fff">
        <circle cx="16" cy="20" r="7" />
        <circle cx="48" cy="20" r="7" />
        <circle cx="32" cy="47" r="7" />
      </g>
      <circle cx="32" cy="20" r="4.5" fill={bg} stroke="#fff" strokeWidth="4" />
    </svg>
  );
}
