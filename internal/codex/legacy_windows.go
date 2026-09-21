//go:build windows

package codex

import (
	"strings"
)

// RemoveLegacy takes down what the PowerShell installer of 2026-09-18 left: the scheduled task «MOX Access Relay»
// with its runner and ssh.exe on the port, the relay key and known_hosts in ~/.ssh, the runner and its log in
// %LOCALAPPDATA%\MOX Access. The environment variables, auth.json and config.toml it wrote are the same ones the
// application manages and are handled by the regular steps. Returns what was found; nothing found is not an error.
func (*Windows) RemoveLegacy() ([]string, error) {
	out, err := powershell(legacyRemoveScript)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, item := range strings.Split(strings.TrimSpace(out), ",") {
		if item = strings.TrimSpace(item); item != "" {
			removed = append(removed, item)
		}
	}
	return removed, nil
}

// The names are the installer's (codex-login.mjs: relayTask, psRelayPaths, psStopRelay). The key file was made
// read-only with icacls, so its ACL is reset before removal. The application's own tunnel runs in process, so the
// ssh.exe filter cannot hit it. Ends only once the port is free: the runner restarts ssh after every exit.
const legacyRemoveScript = `$task = 'MOX Access Relay'
$removed = @()
$sshDir = Join-Path $env:USERPROFILE '.ssh'
$keyFile = Join-Path $sshDir 'mox-relay'; $hostsFile = Join-Path $sshDir 'mox-relay-known_hosts'
$relayDir = Join-Path $env:LOCALAPPDATA 'MOX Access'
$runner = Join-Path $relayDir 'relay.ps1'; $log = Join-Path $relayDir 'relay.log'
if (Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue) {
  Stop-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction SilentlyContinue
  $removed += 'задача планировщика'
}
Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe'" | Where-Object { $_.CommandLine -like '*MOX Access\relay.ps1*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Get-CimInstance Win32_Process -Filter "Name = 'ssh.exe'" | Where-Object { $_.CommandLine -like '*mox-relay*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; $removed += 'туннель' }
foreach ($file in @($runner, $log, $keyFile, $hostsFile)) {
  if (Test-Path $file) { & icacls $file /reset | Out-Null; Remove-Item -Force $file -ErrorAction SilentlyContinue; $removed += (Split-Path $file -Leaf) }
}
for ($i = 0; $i -lt 10; $i++) {
  if (-not (Get-NetTCPConnection -LocalPort 8000 -State Listen -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
}
($removed | Select-Object -Unique) -join ','
`
