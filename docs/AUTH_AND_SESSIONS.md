# Auth and Sessions

## Login flow

- `GET /auth/login` renders the login page.
- `POST /auth/login` binds `email`, `password`, and optional `next`.
- `service.Login()` trims the email, finds the auth user, and checks the bcrypt hash.
- On success the handler creates a session and redirects to `next` or `/`.
- On failure the handler re-renders the login page with a generic error.

## Register flow

- `GET /auth/register` renders the register page.
- `POST /auth/register` validates email, password length, and password confirmation.
- `service.Register()` hashes the password and inserts a member user.
- On success the handler creates a session and redirects.

## Session model

- Sessions are in-memory only.
- Session token is stored in a cookie named `fitness_session`.
- Cookies are `HttpOnly`, `SameSite=Lax`, and scoped to `/`.
- Restarting the app clears sessions.

## Seed admin

- Admin credentials come from `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD`.
- The app upserts the seed admin at startup.
- Default admin is created during bootstrap if the database is empty or the admin already exists.

## Known constraint

- Because sessions are memory-backed, this app is fine for local/dev use, but it is not durable across restarts or multiple replicas.

