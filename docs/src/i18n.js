export const translations = {
  en: {
    nav: {
      install: 'Install',
      features: 'Features',
      how: 'How it works',
    },
    hero: {
      tagline: 'Your terminal, everywhere',
      h1_1: 'Run CLI.',
      h1_2: 'Stream to browser.',
      desc: 'A single binary that wraps any command in a PTY and streams it live over WebSocket. Drop in, drop out — the session stays alive.',
      term_running: 'is running',
      term_server: 'Server',
      term_password: 'Password protection: enabled',
      term_open: 'Open in your browser.',
      term_persist: 'Session persists across reconnects.',
      term_comment1: '# terminal streams to any browser, any device',
      term_comment2: '# close the tab — process keeps running',
    },
    install: {
      title: 'GET STARTED',
      or: 'or',
      buildFromSource: 'build from source',
    },
    values: {
      items: [
        {
          title: 'Access from anywhere',
          desc: "Open a browser on any device and you're connected to your running terminal session.",
        },
        {
          title: 'Multi-command tabs',
          desc: 'Run multiple commands as browser tabs. Drag to reorder, Alt+1~9 to switch. Split any tab to a side pane.',
        },
        {
          title: 'Distributed terminals',
          desc: 'Attach remote servers to a central hub with --attach. Monitor all your machines from a single browser.',
        },
        {
          title: 'Secure by default',
          desc: 'Password auth with Bearer tokens, URL route prefix for reverse proxy, CORS origin checks, and security headers.',
        },
        {
          title: 'File upload',
          desc: 'Upload files to the working directory from the browser. Agent tabs proxy uploads to the remote machine.',
        },
        {
          title: 'Works on mobile',
          desc: 'Touch keyboard with arrow keys, Ctrl combos, and clipboard paste. Responsive split pane as slide-out drawer.',
        },
      ],
    },
    how: {
      title: 'How it works',
      subtitle: 'Four layers, one binary',
      steps: [
        { title: 'Your command', desc: 'Any CLI tool runs in a real pseudo-terminal' },
        { title: 'Server', desc: 'Manages PTY lifecycle, buffers output, handles reconnects' },
        { title: 'WebSocket', desc: 'Bidirectional JSON stream for input, output, and resize' },
        { title: 'Browser', desc: 'xterm.js renders a full terminal with Catppuccin theme' },
      ],
      labels: ['PTY', 'JSON stream', 'Render'],
    },
    cta: {
      title: 'Ready to try?',
      desc_1: 'One command install. One binary to run.',
      desc_2: 'Your terminal, accessible from any browser.',
      install: 'Install rc',
      source: 'View source',
    },
    footer: {
      license: 'MIT License',
      source: 'Source',
      issues: 'Issues',
      releases: 'Releases',
    },
  },
  ko: {
    nav: {
      install: '설치',
      features: '기능',
      how: '작동 방식',
    },
    hero: {
      tagline: '어디서든 터미널을',
      h1_1: 'CLI를 실행하고',
      h1_2: '브라우저로 스트리밍.',
      desc: '바이너리 하나로 어떤 명령이든 PTY에서 실행하고 WebSocket으로 실시간 스트리밍합니다. 탭을 닫아도 프로세스는 계속 살아있습니다.',
      term_running: 'is running',
      term_server: 'Server',
      term_password: 'Password protection: enabled',
      term_open: 'Open in your browser.',
      term_persist: 'Session persists across reconnects.',
      term_comment1: '# 터미널을 어떤 브라우저, 어떤 기기로든 스트리밍',
      term_comment2: '# 탭을 닫아도 프로세스는 계속 실행됩니다',
    },
    install: {
      title: '시작하기',
      or: '또는',
      buildFromSource: '소스에서 빌드',
    },
    values: {
      items: [
        {
          title: '어디서든 접속',
          desc: '브라우저만 있으면 어떤 기기에서든 실행 중인 터미널 세션에 연결됩니다.',
        },
        {
          title: '멀티 커맨드 탭',
          desc: '여러 명령을 브라우저 탭으로 실행합니다. 드래그로 순서 변경, Alt+1~9로 전환. 분할 패널로 동시 모니터링.',
        },
        {
          title: '분산 터미널',
          desc: '--attach로 원격 서버를 중앙 허브에 연결합니다. 모든 머신을 하나의 브라우저에서 모니터링하세요.',
        },
        {
          title: '기본 보안',
          desc: 'Bearer 토큰 인증, 리버스 프록시용 URL 라우트 프리픽스, CORS 출처 검증, 보안 헤더 적용.',
        },
        {
          title: '파일 업로드',
          desc: '브라우저에서 작업 디렉토리로 파일을 업로드합니다. 에이전트 탭은 원격 머신으로 프록시합니다.',
        },
        {
          title: '모바일 지원',
          desc: '화살표 키, Ctrl 조합, 클립보드 붙여넣기 터치 키보드 내장. 반응형 분할 패널은 슬라이드 드로어로.',
        },
      ],
    },
    how: {
      title: '작동 방식',
      subtitle: '4개의 레이어, 하나의 바이너리',
      steps: [
        { title: '실행할 명령', desc: '어떤 CLI 도구든 실제 pseudo-terminal에서 실행' },
        { title: '서버', desc: 'PTY 생명주기 관리, 출력 버퍼링, 재연결 처리' },
        { title: 'WebSocket', desc: '입력, 출력, 리사이즈를 위한 양방향 JSON 스트림' },
        { title: '브라우저', desc: 'xterm.js가 Catppuccin 테마로 완전한 터미널을 렌더링' },
      ],
      labels: ['PTY', 'JSON 스트림', '렌더링'],
    },
    cta: {
      title: '시작해볼까요?',
      desc_1: '명령어 하나로 설치. 바이너리 하나로 실행.',
      desc_2: '브라우저에서 접근 가능한 당신의 터미널.',
      install: 'rc 설치하기',
      source: '소스 보기',
    },
    footer: {
      license: 'MIT 라이선스',
      source: '소스',
      issues: '이슈',
      releases: '릴리즈',
    },
  },
}
