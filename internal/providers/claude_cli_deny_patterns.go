package providers

// ShellDenyPatterns are regex patterns for dangerous shell commands.
//
// This list MUST stay in sync with the canonical deny groups in
// internal/tools/shell_deny_groups.go (DenyGroupRegistry). It is a
// hand-maintained mirror because the providers package cannot import the
// tools package (import cycle); the patterns are compiled into the Claude
// CLI PreToolUse hook (claude_cli_hooks.go) which gates the `claude`
// binary's native Bash tool via grep -E. Keep the groups and ordering
// identical to DenyGroupRegistry so the CLI hook is never a weaker
// boundary than the primary exec path. When you change one list, change
// the other in the same commit.
var ShellDenyPatterns = []string{
	// Destructive operations
	`\brm\s+-[rf]{1,2}\b`,
	`\brm\s+.*--recursive`,
	`\brm\s+.*--force`,
	`\bdel\s+/[fq]\b`,
	`\brmdir\s+/s\b`,
	`\b(mkfs|diskpart)\b|\bformat\s+(?:/dev/|[a-zA-Z]:)`, // only disk/device targets, not benign "format ..."
	`\bdd\s+if=`,
	`>\s*/dev/sd[a-z]\b`,
	`\bfind\b.*\s-delete\b`,  // recursive delete via find
	`\bfind\b.*-exec\s+rm\b`, // recursive delete via find -exec rm
	`\b(shutdown|reboot|poweroff|halt)\b`,
	`\b(init|telinit)\s+[06]\b`,          // SysV shutdown/reboot
	`\bsystemctl\s+(suspend|hibernate)\b`, // power management
	`:\(\)\s*\{.*\};\s*:`,                 // fork bomb

	// Data exfiltration
	`\bcurl\b.*\|\s*(ba)?sh\b`,
	`\bcurl\b.*(-d\b|-F\b|--data|--upload|--form|-T\b|(-X|--request)\s*P(UT|OST|ATCH))`,
	`\bwget\b.*-O\s*-\s*\|\s*(ba)?sh\b`,
	`\bwget\b.*(--post-(data|file)|--method=P(UT|OST|ATCH)|--body-data)`,
	`\b(nslookup|dig|host)\b`,
	`/dev/tcp/`,
	`\b(curl|wget)\b.*\blocalhost\b`,
	`\b(curl|wget)\b.*\b127\.0\.0\.1\b`,
	`\b(curl|wget)\b.*\b0\.0\.0\.0\b`,

	// Reverse shells
	`\b(nc|ncat|netcat)\b.*(\s+-[a-z]|\s+[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+|\s+localhost\b)`, // \d -> [0-9] for grep -E (POSIX ERE) compatibility in the CLI hook
	`\bsocat\b`,
	`\bopenssl\b.*s_client`,
	`\btelnet\b.*[0-9]+`,
	`\bpython[23]?\b.*(import|from)\s+(socket|http|urllib|requests|httpx|aiohttp)\b`,
	`\bperl\b.*-e\s*.*\b[Ss]ocket\b`,
	`\bruby\b.*-e\s*.*\b(TCPSocket|Socket)\b`,
	`\bnode\b.*-e\s*.*\b(net\.|http\.|https\.|fetch\(|axios|got\(|undici)\b`,
	`\bnode\b.*-e\s*.*require\s*\(\s*['"]https?['"]\s*\)`,
	`\bawk\b.*/inet/`,
	`\bmkfifo\b`,

	// Code injection & eval
	`\beval\s*\$`,
	`\bbase64\s+(-d\w*|--decode)\b.*\|\s*(ba)?sh\b`,

	// Privilege escalation
	`\bsudo\b`,
	`\bsu\b`,
	`\bdoas\b`,
	`\bpkexec\b`,
	`\brunuser\b`,
	`\bnsenter\b`,
	`\bunshare\b`,
	`\b(mount|umount)\b`,
	`\b(capsh|setcap|getcap)\b`,

	// Dangerous path operations
	`\bchmod\s+[0-7]{3,4}\s+/`,
	`\bchown\b.*\s+/`,
	`\bchmod\b.*\+x.*/tmp/`,
	`\bchmod\b.*\+x.*/var/tmp/`,
	`\bchmod\b.*\+x.*/dev/shm/`,

	// Environment variable injection
	`\bLD_PRELOAD\s*=`,
	`\bDYLD_INSERT_LIBRARIES\s*=`,
	`\bLD_LIBRARY_PATH\s*=`,
	`/etc/ld\.so\.preload`,
	`\bGIT_EXTERNAL_DIFF\s*=`,
	`\bGIT_DIFF_OPTS\s*=`,
	`\bBASH_ENV\s*=`,
	`\bENV\s*=.*\bsh\b`,
	`\bexport\s+LD_`,
	`\bexport\s+DYLD_`,
	`\bexport\s+BASH_ENV\b`,
	`\bexport\s+ENV\s*=`,
	`\bexport\s+PROMPT_COMMAND\b`,

	// Container escape
	`/var/run/docker\.sock|docker\.(sock|socket)`,
	`/proc/sys/(kernel|fs|net)/`,
	`/sys/(kernel|fs|class|devices)/`,

	// Crypto mining
	`\b(xmrig|cpuminer|minerd|cgminer|bfgminer|ethminer|nbminer|t-rex|phoenixminer|lolminer|gminer|claymore)\b`,
	`stratum\+tcp://|stratum\+ssl://`,

	// Filter bypass (CVE mitigations)
	`\bsed\b.*['"]/e\b`,
	`\bsort\b.*--compress-program`,
	`\bgit\b.*(--upload-pack|--receive-pack|--exec)=`,
	`\b(rg|grep)\b.*--pre=`,
	`\bman\b.*--html=`,
	`\bhistory\b.*-[saw]\b`,
	`\$\{[^}]*@[PpEeAaKk]\}`,

	// Network reconnaissance & tunneling
	`\b(nmap|masscan|zmap|rustscan)\b`,
	`\b(ssh|scp|sftp)\b.*@`,
	`\b(chisel|frp|ngrok|cloudflared|bore|localtunnel)\b`,

	// Package installation
	`\bpip[0-9.]*\s+install\b`, // pip, pip3, pip3.11, ...
	`\bnpm\s+install\b`,
	`\bnpm\s+i\b`,
	`\bnpx\b`,               // npx <pkg> runs an arbitrary package
	`\b(pnpm|yarn)\s+dlx\b`, // pnpm/yarn dlx <pkg>
	`\bapk\s+(add|del)\b`,
	`\bdoas\s+apk\b`,
	`\byarn\s+(add|global)\b`,
	`\bpnpm\s+(add|install)\b`,
	`\bpip[0-9.]*\s+uninstall\b`,
	`\bnpm\s+uninstall\b`,
	`\bpython[23]?\b.*-m\s+pip\b`,

	// Persistence
	`\bcrontab\b`,
	`>\s*~/?\.(bashrc|bash_profile|profile|zshrc)`,
	`\btee\b.*\.(bashrc|bash_profile|profile|zshrc)`,

	// Process manipulation
	`\bkill\s+-9\s`,
	`\b(killall|pkill)\b`,

	// Environment variable dumping
	`^\s*env\s*$`,
	`^\s*env\s*\|`,
	`^\s*env\s*>\s`,
	`\bprintenv\b`,
	`^\s*(set|export\s+-p|declare\s+-[px])\s*($|\|)`,
	`\bdeclare\s+-[px]\b`, // declare -p VAR / declare -x FOO dumps a single var's value
	`\bcompgen\s+-e\b`,
	`/proc/[^/]+/environ`,
	`/proc/self/environ`,
	`(?i)\bstrings\b.*/proc/`,
	// GOCLAW secret reads. The canonical group prefixes echo/printf with (?i),
	// but the CLI hook matches via grep -E (busybox/POSIX ERE in the runtime
	// image) which does NOT honor a (?i) inline flag — it would treat the flag
	// as literal text and silently never match. The realistic exfil command has
	// lowercase echo/printf + uppercase GOCLAW_, so the case-sensitive form
	// below is what actually fires here.
	`\becho\b.*\$\{?GOCLAW_(GATEWAY_TOKEN|ENCRYPTION_KEY|POSTGRES_DSN)`,
	`\bprintf\b.*\$\{?GOCLAW_(GATEWAY_TOKEN|ENCRYPTION_KEY|POSTGRES_DSN)`,
	`\bpython[23]?\b.*os\.(environ|getenv).*GOCLAW_`,
	`\bnode\b.*-e.*process\.env\.GOCLAW_`,
}
