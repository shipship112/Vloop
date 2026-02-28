# Vloop

A short-video Feed system based on Go (backend + frontend), including account, video, like, comment, follow and Feed flow. Supports Redis cache and RabbitMQ asynchronous Worker (API process and Worker process can be deployed separately).

## One‑Click Startup with Docker Compose (Recommended)

Requirements: Docker Desktop / Docker Engine + Docker Compose installed.

```bash
docker compose up -d --build
```

Access:
- Frontend: `http://localhost:5173`
- Backend API: `http://localhost:8080`
- RabbitMQ management console: `http://localhost:15672` (default account `admin` / `password123`)

Description:
- Compose will start `mysql`, `redis`, `rabbitmq`, `backend` (API), `worker`, `frontend`.
- The backend configuration inside the container uses `backend/configs/config.docker.yaml` (mounted to `/app/configs/config.yaml`).

## Local Development Startup (Non‑Containerized)

1) Start dependencies first (you can also use compose to start dependencies only):
```bash
docker compose up -d mysql redis rabbitmq
```

2) Start backend API:
```bash
cd backend
go run ./cmd
```

3) Start Worker (consume MQ, async write to database / update Redis hot list):
```bash
cd backend
go run ./cmd/worker
```

4) Start frontend (development mode):
```bash
cd frontend
npm install
npm run dev
```

The frontend uses Vite to proxy `/api` to `http://127.0.0.1:8080` by default (see `frontend/vite.config.ts`).
