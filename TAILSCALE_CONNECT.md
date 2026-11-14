# Как открыть тестового Бобби через Tailscale

1. [Установите Tailscale](https://tailscale.com/download) на свой компьютер и войдите в **ту же сеть Tailscale**, где находится сервер. Если нужная сеть не видна, попросите её администратора добавить ваш аккаунт. [Инструкции для macOS](https://tailscale.com/docs/install/mac) и [Windows](https://tailscale.com/docs/install/windows).
2. Убедитесь, что сервер доступен: в терминале выполните `tailscale status` или `tailscale ping 100.105.236.12`.
3. Откройте тестовый чат: **http://100.105.236.12:18001/**.

Для входа на сам сервер по SSH: `ssh hackchange_collab@100.105.236.12` (нужен доступ к этой учётной записи). Если чат не открывается, проверьте подключение в [справке Tailscale](https://tailscale.com/docs/reference/troubleshooting/connectivity/connect-device-failure).
