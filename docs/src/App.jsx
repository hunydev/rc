import { useState, useEffect, createContext, useContext } from 'react'
import {
  Terminal,
  Globe,
  Smartphone,
  Copy,
  Check,
  Github,
  Server,
  Shield,
  Upload,
  ExternalLink,
  BookOpen,
  Languages,
  Tag,
  SplitSquareHorizontal,
} from 'lucide-react'
import { translations } from './i18n'
import DocsPage from './DocsPage.jsx'
import './App.css'

const INSTALL_CMD = 'curl -fsSL https://rc.huny.dev/install.sh | bash'
const INSTALL_CMD_WIN = 'powershell -c "irm https://rc.huny.dev/install.ps1 | iex"'
const GITHUB_URL = 'https://github.com/hunydev/rc'

const LangContext = createContext()
function useLang() { return useContext(LangContext) }
function t(lang, path) {
  return path.split('.').reduce((o, k) => o?.[k], translations[lang])
}

function useLatestVersion() {
  const [version, setVersion] = useState(null)
  useEffect(() => {
    fetch('https://api.github.com/repos/hunydev/rc/releases/latest')
      .then(r => r.json())
      .then(d => { if (d.tag_name) setVersion(d.tag_name) })
      .catch(() => {})
  }, [])
  return version
}

function CopyButton({ text }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button className={`copy-btn ${copied ? 'copied' : ''}`} onClick={handleCopy}>
      {copied ? <><Check size={14} /> Copied</> : <><Copy size={14} /> Copy</>}
    </button>
  )
}

function LangToggle({ lang, setLang }) {
  return (
    <button
      className="lang-toggle"
      onClick={() => setLang(lang === 'en' ? 'ko' : 'en')}
      title={lang === 'en' ? '한국어로 전환' : 'Switch to English'}
    >
      <Languages size={14} />
      {lang === 'en' ? 'KO' : 'EN'}
    </button>
  )
}

function Nav() {
  const { lang, setLang } = useLang()
  return (
    <nav className="nav">
      <a href="#" className="nav-brand">
        <img src="/logo.svg" alt="rc" className="logo-img" />
        <span>rc</span>
      </a>
      <div className="nav-right">
        <a href="#install" className="nav-link">{t(lang, 'nav.install')}</a>
        <a href="#features" className="nav-link">{t(lang, 'nav.features')}</a>
        <a href="#how" className="nav-link">{t(lang, 'nav.how')}</a>
        <a href="#/docs" className="nav-link nav-link-docs">{t(lang, 'nav.docs')}</a>
        <LangToggle lang={lang} setLang={setLang} />
        <a href={GITHUB_URL} target="_blank" rel="noopener noreferrer" className="nav-github">
          <Github size={14} />
          GitHub
        </a>
      </div>
    </nav>
  )
}

function TerminalMockup() {
  const { lang } = useLang()
  const h = translations[lang].hero
  return (
    <div className="terminal-mockup">
      <div className="terminal-titlebar">
        <div className="terminal-dots">
          <span className="td-r" />
          <span className="td-y" />
          <span className="td-g" />
        </div>
        <div className="terminal-title">rc — localhost:8000</div>
        <div style={{ width: 50 }} />
      </div>
      <div className="terminal-body">
        <div className="line"><span className="prompt">$ </span><span className="cmd">rc </span><span className="flag">-c </span><span className="val">bash </span><span className="flag">-c </span><span className="val">htop </span><span className="flag">--password </span><span className="val">****</span></div>
        <div className="blank" />
        <div className="line"><span className="output">  </span><span className="success">rc</span><span className="output"> {h.term_running}</span></div>
        <div className="line"><span className="output">  {h.term_server}  </span><span className="url">http://0.0.0.0:8000</span></div>
        <div className="line"><span className="output">    Tab 0: bash</span></div>
        <div className="line"><span className="output">    Tab 1: htop</span></div>
        <div className="line"><span className="output">    {h.term_password}</span></div>
        <div className="blank" />
        <div className="line"><span className="output">  {h.term_open}</span></div>
        <div className="line"><span className="output">  {h.term_persist}</span></div>
        <div className="blank" />
        <div className="line"><span className="comment">  {h.term_comment1}</span></div>
        <div className="line"><span className="comment">  {h.term_comment2}</span></div>
        <div className="line"><span className="prompt">  </span><span className="cursor" /></div>
      </div>
    </div>
  )
}

function Hero() {
  const { lang } = useLang()
  const h = translations[lang].hero
  const version = useLatestVersion()
  return (
    <section className="hero">
      <div className="hero-badges">
        <div className="hero-tagline">
          <Terminal size={14} />
          {h.tagline}
        </div>
        {version && (
          <a href={`${GITHUB_URL}/releases/tag/${version}`} target="_blank" rel="noopener noreferrer" className="version-badge">
            <Tag size={12} />
            {version}
          </a>
        )}
      </div>
      <h1>
        {h.h1_1}<br />
        <span className="accent">{h.h1_2}</span>
      </h1>
      <p className="hero-desc">{h.desc}</p>
      <TerminalMockup />
    </section>
  )
}

function InstallStrip() {
  const { lang } = useLang()
  const ins = translations[lang].install
  const [tab, setTab] = useState('unix')
  return (
    <section className="install-strip" id="install">
      <h2>{ins.title}</h2>
      <div className="install-tabs">
        <button className={`install-tab ${tab === 'unix' ? 'active' : ''}`} onClick={() => setTab('unix')}>macOS / Linux</button>
        <button className={`install-tab ${tab === 'win' ? 'active' : ''}`} onClick={() => setTab('win')}>Windows</button>
      </div>
      {tab === 'unix' ? (
        <div className="install-box">
          <code>
            <span className="dollar">$</span>
            <span className="ic">curl</span>
            {' -fsSL '}
            <span className="iu">https://rc.huny.dev/install.sh</span>
            <span className="ip">{' | '}</span>
            <span className="ic">bash</span>
          </code>
          <CopyButton text={INSTALL_CMD} />
        </div>
      ) : (
        <div className="install-box">
          <code>
            <span className="dollar">&gt;</span>
            <span className="ic">powershell</span>
            {' -c '}
            <span className="iu">"irm https://rc.huny.dev/install.ps1 | iex"</span>
          </code>
          <CopyButton text={INSTALL_CMD_WIN} />
        </div>
      )}
      <p className="install-or">
        {ins.or} <a href={`${GITHUB_URL}#quick-start`} target="_blank" rel="noopener noreferrer">{ins.buildFromSource}</a>
      </p>
    </section>
  )
}

function Values() {
  const { lang } = useLang()
  const items = translations[lang].values.items
  const icons = [
    { icon: <Globe size={20} />, color: 'var(--blue)', bg: 'rgba(137, 180, 250, 0.1)' },
    { icon: <SplitSquareHorizontal size={20} />, color: 'var(--green)', bg: 'rgba(166, 227, 161, 0.1)' },
    { icon: <Server size={20} />, color: 'var(--mauve)', bg: 'rgba(203, 166, 247, 0.1)' },
    { icon: <Shield size={20} />, color: 'var(--peach)', bg: 'rgba(250, 179, 135, 0.1)' },
    { icon: <Upload size={20} />, color: 'var(--yellow)', bg: 'rgba(249, 226, 175, 0.1)' },
    { icon: <Smartphone size={20} />, color: 'var(--teal)', bg: 'rgba(148, 226, 213, 0.1)' },
  ]

  return (
    <section className="values" id="features">
      <div className="values-grid">
        {items.map((item, i) => (
          <div className="value-cell" key={i}>
            <div className="value-icon" style={{ background: icons[i].bg, color: icons[i].color }}>
              {icons[i].icon}
            </div>
            <h3>{item.title}</h3>
            <p>{item.desc}</p>
          </div>
        ))}
      </div>
    </section>
  )
}

function HowItWorks() {
  const { lang } = useLang()
  const how = translations[lang].how
  const colors = [
    { color: 'var(--mauve)', bg: 'rgba(203, 166, 247, 0.1)' },
    { color: 'var(--green)', bg: 'rgba(166, 227, 161, 0.1)' },
    { color: 'var(--yellow)', bg: 'rgba(249, 226, 175, 0.1)' },
    { color: 'var(--blue)', bg: 'rgba(137, 180, 250, 0.1)' },
  ]
  const icons = [
    <Terminal size={20} />,
    <Server size={20} />,
    <Globe size={20} />,
    <Smartphone size={20} />,
  ]

  return (
    <section className="how-section" id="how">
      <div className="how-header">
        <h2>{how.title}</h2>
        <p>{how.subtitle}</p>
      </div>
      <div className="how-grid">
        {how.steps.map((step, i) => (
          <div className="how-card" key={i}>
            <div className="how-card-top">
              <div className="how-card-icon" style={{ background: colors[i].bg, color: colors[i].color }}>
                {icons[i]}
              </div>
              <span className="how-step-num" style={{ color: colors[i].color }}>{String(i + 1).padStart(2, '0')}</span>
            </div>
            <h4>{step.title}</h4>
            <p>{step.desc}</p>
          </div>
        ))}
      </div>
    </section>
  )
}

function CTA() {
  const { lang } = useLang()
  const c = translations[lang].cta
  return (
    <section className="cta-section">
      <div className="cta-card">
        <h2>{c.title}</h2>
        <p>{c.desc_1}<br />{c.desc_2}</p>
        <div className="cta-actions">
          <a href="#install" className="btn-primary">
            <Terminal size={16} />
            {c.install}
          </a>
          <a href={GITHUB_URL} target="_blank" rel="noopener noreferrer" className="btn-ghost">
            <Github size={16} />
            {c.source}
          </a>
        </div>
      </div>
    </section>
  )
}

function Footer() {
  const { lang } = useLang()
  const f = translations[lang].footer
  const version = useLatestVersion()
  return (
    <footer className="footer">
      <div className="footer-left">
        <span className="rc-name">rc</span>
        {version && <span className="footer-version">{version}</span>}
        <span>{f.license}</span>
      </div>
      <div className="footer-links">
        <a href="#/docs" className="footer-link-docs"><BookOpen size={13} /> {f.docs}</a>
        <a href={GITHUB_URL} target="_blank" rel="noopener noreferrer"><Github size={13} /> {f.source}</a>
        <a href={`${GITHUB_URL}/issues`} target="_blank" rel="noopener noreferrer"><BookOpen size={13} /> {f.issues}</a>
        <a href={`${GITHUB_URL}/releases`} target="_blank" rel="noopener noreferrer"><ExternalLink size={13} /> {f.releases}</a>
      </div>
    </footer>
  )
}

export default function App() {
  const [lang, setLang] = useState('en')
  const [page, setPage] = useState(() => {
    return window.location.hash.startsWith('#/docs') ? 'docs' : 'home'
  })

  useEffect(() => {
    const handleHash = () => {
      setPage(window.location.hash.startsWith('#/docs') ? 'docs' : 'home')
    }
    window.addEventListener('hashchange', handleHash)
    return () => window.removeEventListener('hashchange', handleHash)
  }, [])

  const goHome = () => {
    window.location.hash = ''
    setPage('home')
  }

  if (page === 'docs') {
    return (
      <LangContext.Provider value={{ lang, setLang }}>
        <DocsPage onBack={goHome} />
      </LangContext.Provider>
    )
  }

  return (
    <LangContext.Provider value={{ lang, setLang }}>
      <div className="app">
        <Nav />
        <Hero />
        <div className="divider" />
        <InstallStrip />
        <div className="divider" />
        <Values />
        <div className="divider" />
        <HowItWorks />
        <CTA />
        <Footer />
      </div>
    </LangContext.Provider>
  )
}
