# Настройка проекта

[English (default)](../../PROJECT-SETUP.md) | Русский

Система не привязана к владельцу своего исходного кода, проекту или GitHub-аккаунту.
Каждая установка выбирает источник проекта, базовую ветку, аккаунт, модели и
интеграции в своей локальной конфигурации. Мастер не запускает агентов.

```powershell
.\scripts\Configure-Project.ps1
```

Мастер предлагает Docker или ограниченный Windows-режим, затем существующий
GitHub-репозиторий или новый локальный проект. Setup и Start также вызывают мастер,
если файл конфигурации отсутствует. Существующая конфигурация не перезаписывается
без явного ввода `REPLACE`. Для разных проектов используйте отдельные файлы,
состояние, рабочие каталоги, доски и Telegram-ботов.

## Существующий GitHub-репозиторий

```powershell
.\scripts\Configure-Project.ps1 -Mode github -Backend docker `
  -Config config.team.json -Repository OWNER/REPOSITORY `
  -GitHubUser LOGIN -BaseBranch develop
```

Если выбранного аккаунта нет в GitHub CLI, мастер запускает авторизацию через
device flow. Нужно войти именно под выбранным аккаунтом. Затем проверяются
фактический login, право записи в репозиторий и существование базовой ветки.
Активный аккаунт CLI и переменные токенов не определяют рабочую учётную запись.
Токен хранится средствами GitHub CLI, не записывается в конфигурацию проекта.
Эти проверки повторяются контроллером до старта агентов.

Для нового репозитория исходный проект клонируется только доверенным брокером
после запуска задач. Мастер существующего репозитория не создаёт копию проекта.
Коммиты идут исключительно в ветки `codex/...`; PR создаются в выбранную ветку.
Done означает создание PR. Ревьюеры оставляют комментарии, не Approve и не merge.

## Новый локальный проект

```powershell
.\scripts\Configure-Project.ps1 -Mode local -Backend native-windows `
  -Config config.local.json -LocalPath C:\Projects\new-project `
  -BaseBranch main -LocalOnly
```

Выбирается новый пустой каталог. Мастер создаёт минимальный проект, первый коммит
и отдельное bare Git-хранилище `.orchestrator.git`. Существующие файлы не добавляются
в Git автоматически. Без `-LocalOnly` мастер предложит вместо локальной работы
создать приватный GitHub-репозиторий. Также это можно выбрать через `-Publish`:

```powershell
.\scripts\Configure-Project.ps1 -Mode local -Backend docker -Publish `
  -Config config.new.json -LocalPath C:\Projects\another-project `
  -Repository OWNER/NEW-REPOSITORY -GitHubUser LOGIN -BaseBranch main
```

В полностью локальном режиме GitHub CLI и GitHub-авторизация не используются.
Исходные коммиты передаются брокеру Git bundle без монтирования каталога проекта
в контейнер. Результаты возвращаются в bare-хранилище как локальные ветки. Это
работает с обоими вариантами изоляции. Ревью сохраняется в `state_dir/reviews`:
JSON с описанием задачи и Markdown с замечаниями для конкретного SHA.
Done означает сохранение локальной ветки и постановку на локальное ревью;
Telegram-уведомление о завершении приходит после ревью без замечаний.

Посмотреть и принять результат вручную из каталога локального проекта:

```powershell
git --git-dir=.orchestrator.git branch --list
git fetch .orchestrator.git codex/TASK-ID
git diff main...FETCH_HEAD
git merge FETCH_HEAD
```

Система не меняет базовую ветку и не выполняет merge. После ручного принятия
синхронизируйте базовую ветку хранилища, чтобы новые задания получили новый исходник:

```powershell
git --git-dir=.orchestrator.git fetch . main:main
```

При изменении ветки задания человеком автоматическое исправление останавливается.
Состояние одного проекта нельзя использовать для другого проекта, аккаунта или
базовой ветки. Docker-контейнеры и volumes имеют отдельные имена для каждого проекта.

## Завершение установки

Заполните Trello, Telegram и авторизацию Codex. Затем выполните Setup для выбранной
изоляции и Start с тем же файлом конфигурации. Для native Windows:

```powershell
.\scripts\Setup-Native.ps1 -Config config.local.json
.\scripts\Start-Orchestrator.ps1 -Config config.local.json -Check
.\scripts\Start-Orchestrator.ps1 -Config config.local.json
```

Модели и reasoning effort задаются конфигурацией. Значения в шаблоне — исходные
настройки, а не ограничения на доступные модели. Максимум два исполнителя и запрет
Approve/merge остаются правилами безопасности системы.
