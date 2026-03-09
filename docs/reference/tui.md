# TUI Dashboard

Running `sympozium` with no arguments launches a **k9s-style interactive terminal UI** for full cluster-wide agentic management.

## Views

| Key | View | Description |
|-----|------|-------------|
| `1` | Personas | PersonaPack list — press Enter to activate a pack and create agents |
| `2` | Instances | SympoziumInstance list with status, channels, memory config |
| `3` | Runs | AgentRun list with phase, duration, result preview |
| `4` | Policies | SympoziumPolicy list with feature gates |
| `5` | Skills | SkillPack list with file counts |
| `6` | Channels | Channel pod status (Telegram, Slack, Discord, WhatsApp) |
| `7` | Schedules | SympoziumSchedule list with cron, type, phase, run count |
| `8` | Pods | All sympozium pods with status and restarts |

## Keybindings

| Key | Action |
|-----|--------|
| `l` | View logs for the selected resource |
| `d` | Describe the selected resource (kubectl describe) |
| `x` | Delete the selected resource (with confirmation) |
| `e` | Edit the selected instance (skills, memory, schedule) |
| `s` | Toggle skills on the selected instance |
| `Enter` | View details / select row |
| `Tab` | Cycle between views |
| `Esc` | Go back / close panel |
| `?` | Toggle help |

## Slash Commands

| Command | Description |
|---------|-------------|
| `/run <task>` | Create and submit an AgentRun |
| `/schedule <instance> <cron> <task>` | Create a SympoziumSchedule |
| `/memory <instance>` | View persistent memory for an instance |
| `/personas` | Switch to PersonaPacks view |
| `/instances` `/runs` `/channels` `/schedules` | Switch views |
| `/delete <type> <name>` | Delete a resource with confirmation |
