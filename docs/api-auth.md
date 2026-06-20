# API — Авторизация и профиль (M1)

Справочник по HTTP-ручкам блока auth + `/me` для ручного тестирования (Postman).

- **Base URL (dev):** `http://localhost:8080`
- **Префикс API:** `/api/v1`
- **Формат:** JSON (`Content-Type: application/json`).
- **Авторизация:** access-токен в заголовке `Authorization: Bearer <access_token>`.
- **Refresh-токен:** живёт в `httpOnly`-cookie `refresh_token` (Postman хранит её в cookie jar автоматически — отдельно подставлять не нужно). Для удобства `refresh`/`logout` также принимают токен в теле.

## Единый формат ошибки

Все ошибки приходят в одном виде:

```json
{ "error": { "code": "validation_error", "message": "Неверные данные запроса" } }
```

| code | HTTP | Когда |
|---|---|---|
| `validation_error` | 400 | Тело не прошло валидацию DTO |
| `unauthorized` | 401 | Нет/неверный/просроченный токен |
| `forbidden` | 403 | Недостаточно прав (не админ) |
| `not_found` | 404 | Ресурс не найден |
| `conflict` | 409 | Дубликат (email уже занят) |
| `internal_error` | 500 | Внутренняя ошибка |

---

## Служебные

### `GET /health`
Проверка БД. Без авторизации.

**200** `{ "status": "healthy", "db": "up" }` · **503** при недоступной БД.

### `GET /api/v1/ping`
**200** `{ "message": "pong" }`

---

## Auth (без авторизации)

### `POST /api/v1/auth/register`
Регистрация по email+паролю. Создаёт пользователя, кошелёк и начисляет `signup_bonus` (по умолчанию 10000).

**Тело:**
```json
{
  "email": "test@example.com",
  "password": "password123",
  "display_name": "Тестер"
}
```
Валидация: `email` — корректный email; `password` — min 8; `display_name` — min 2.

**200 OK:**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6...",
  "token_type": "bearer",
  "expires_in": 900
}
```
+ заголовок `Set-Cookie: refresh_token=...; HttpOnly; Path=/`.

**Ошибки:** `400 validation_error`, `409 conflict` (email уже зарегистрирован).

---

### `POST /api/v1/auth/login`
Вход по email+паролю.

**Тело:**
```json
{ "email": "test@example.com", "password": "password123" }
```

**200 OK:** как у `register` (access в теле, refresh в cookie).

**Ошибки:** `400 validation_error`, `401 unauthorized` (неверный email или пароль — без уточнения причины).

---

### `POST /api/v1/auth/refresh`
Обмен refresh-токена на новый access. Токен берётся из cookie `refresh_token`; если cookie нет — из тела.

**Тело (опционально, для Postman):**
```json
{ "refresh_token": "<plain refresh token>" }
```

**200 OK:** новый `access_token` (тело как у `login`), cookie переписывается.

**Ошибки:** `401 unauthorized` (токен отсутствует / не найден / отозван / истёк).

---

### `POST /api/v1/auth/logout`
Отзывает refresh-токен и стирает cookie. Идемпотентно (повторный вызов не ошибка). Тело не требуется (токен из cookie), либо `{ "refresh_token": "..." }`.

**200 OK:**
```json
{ "message": "Вы вышли из системы" }
```

---

## Профиль `/me` (нужен `Authorization: Bearer <access_token>`)

Без валидного access-токена все три ручки → `401 unauthorized`.

### `GET /api/v1/me`
Профиль + баланс.

**200 OK:**
```json
{
  "user": {
    "id": 1,
    "email": "test@example.com",
    "display_name": "Тестер",
    "avatar_url": null,
    "role": "user",
    "created_at": "2026-06-20T12:00:00Z",
    "last_login_at": null
  },
  "balance": 10000
}
```
> `password_hash` наружу не отдаётся.

---

### `PATCH /api/v1/me`
Частичное обновление профиля. Любое поле можно опустить — оно сохранит текущее значение.

**Тело:**
```json
{ "display_name": "Новое имя", "avatar_url": "https://cdn/x.png" }
```
Валидация: `display_name` (если передан) — min 2; `avatar_url` — строка/`null`.

**200 OK:** обновлённый объект как в `GET /me`.

**Ошибки:** `400 validation_error`, `401 unauthorized`.

---

### `GET /api/v1/me/transactions?page=1`
История движений по кошельку (новые — первыми). `page` опционален (по умолчанию 1, размер страницы 50).

**200 OK:**
```json
{
  "page": 1,
  "items": [
    {
      "id": 1,
      "amount": 10000,
      "type": "signup_bonus",
      "bet_id": null,
      "balance_after": 10000,
      "created_at": "2026-06-20T12:00:00Z"
    }
  ]
}
```

---

## Сценарий проверки в Postman

1. `POST /auth/register` → получить `access_token`, refresh уедет в cookie jar.
2. В переменную окружения положить `access_token` (можно тестом: `pm.environment.set("access_token", pm.response.json().access_token)`).
3. `GET /me` с заголовком `Authorization: Bearer {{access_token}}` → баланс `10000`.
4. `GET /me/transactions` → запись `signup_bonus +10000`.
5. `POST /auth/refresh` (cookie подставится сама) → новый `access_token`.
6. `POST /auth/logout` → refresh отозван; повторный `refresh` теперь даёт `401`.
