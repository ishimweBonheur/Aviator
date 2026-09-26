# Aviator

Local backend and existing React frontend: see [LOCAL_SYSTEM.md](LOCAL_SYSTEM.md) for setup, API contracts, Redis coordination, recovery and validation.

With the frontend at `../Aviator-frontend`, run `docker compose up --build -d`.
Open `http://localhost:5173`; Swagger is at `http://localhost:7000/swagger/index.html`.

The admin dashboard is at `/admin` in the same frontend. See [ADMIN.md](ADMIN.md) for first-admin setup, API contracts, analytics definitions, and validation commands.
