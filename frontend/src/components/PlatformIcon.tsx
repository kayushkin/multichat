interface Props {
  platform: string
  size?: number
  className?: string
}

export default function PlatformIcon({ platform, size = 20, className = '' }: Props) {
  const s = size
  switch (platform) {
    case 'telegram':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#26A5E4" />
          <path d="M5.43 11.87l11.07-4.28c.51-.19.96.13.79.72l-1.88 8.87c-.14.63-.51.78-.84.49l-2.68-1.98-1.29 1.24c-.14.14-.26.26-.54.26l.19-2.73 4.96-4.48c.22-.19-.05-.3-.34-.12l-6.13 3.86-2.64-.82c-.57-.18-.58-.57.12-.85z" fill="#fff" />
        </svg>
      )
    case 'whatsapp':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#25D366" />
          <path d="M17.47 14.38c-.28-.14-1.65-.81-1.9-.9-.26-.1-.44-.14-.63.14-.19.28-.73.9-.89 1.09-.16.19-.33.21-.61.07-.28-.14-1.18-.44-2.25-1.39-.83-.74-1.39-1.66-1.56-1.94-.16-.28-.02-.43.12-.57.13-.13.28-.33.42-.5.14-.16.19-.28.28-.47.1-.19.05-.35-.02-.5-.07-.14-.63-1.52-.86-2.08-.23-.55-.46-.47-.63-.48h-.54c-.19 0-.5.07-.76.35s-1 .98-1 2.38 1.02 2.76 1.17 2.95c.14.19 2.01 3.07 4.87 4.3.68.3 1.21.47 1.63.6.68.22 1.3.19 1.79.11.55-.08 1.65-.67 1.88-1.32.23-.65.23-1.21.16-1.32-.07-.12-.26-.19-.54-.33z" fill="#fff" />
        </svg>
      )
    case 'signal':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#3A76F0" />
          <path d="M6.2 17.8l.9-3.2A5.9 5.9 0 016 11.4C6 8.4 8.7 6 12 6s6 2.4 6 5.4-2.7 5.4-6 5.4c-1.1 0-2.2-.3-3.1-.8L6.2 17.8z" fill="#fff" />
        </svg>
      )
    case 'facebook':
    case 'messenger':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#0084FF" />
          <path d="M12 6C8.69 6 6 8.45 6 11.5c0 1.73.86 3.27 2.2 4.28V18l2.17-1.19c.58.16 1.19.25 1.83.25 3.31 0 6-2.45 6-5.5S15.31 6 12 6zm.6 7.4l-1.53-1.63L8.01 13.4l3.36-3.57 1.53 1.63 3.06-1.63-3.36 3.57z" fill="#fff" />
        </svg>
      )
    case 'imessage':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#34C759" />
          <path d="M12 6C8.13 6 5 8.69 5 12c0 1.66.72 3.17 1.9 4.3L6.5 18l1.9-.7C9.5 17.7 10.7 18 12 18c3.87 0 7-2.69 7-6s-3.13-6-7-6z" fill="#fff" />
        </svg>
      )
    case 'instagram':
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <defs>
            <linearGradient id="ig" x1="0" y1="1" x2="1" y2="0">
              <stop offset="0%" stopColor="#FFDC80" />
              <stop offset="25%" stopColor="#F77737" />
              <stop offset="50%" stopColor="#E1306C" />
              <stop offset="75%" stopColor="#C13584" />
              <stop offset="100%" stopColor="#833AB4" />
            </linearGradient>
          </defs>
          <circle cx="12" cy="12" r="12" fill="url(#ig)" />
          <rect x="7" y="7" width="10" height="10" rx="3" fill="none" stroke="#fff" strokeWidth="1.5" />
          <circle cx="12" cy="12" r="2.5" fill="none" stroke="#fff" strokeWidth="1.5" />
          <circle cx="15.5" cy="8.5" r="0.8" fill="#fff" />
        </svg>
      )
    default:
      return (
        <svg width={s} height={s} viewBox="0 0 24 24" className={className}>
          <circle cx="12" cy="12" r="12" fill="#6B7280" />
          <path d="M8 12h8M12 8v8" stroke="#fff" strokeWidth="2" strokeLinecap="round" />
        </svg>
      )
  }
}
