# Environment Configuration

This directory contains environment templates for td-sync deployments.

## Quick Start

```bash
# 1. Copy the template for your target environment
cp .env.prod.example .env.prod

# 2. Edit with your values
vim .env.prod

# 3. Deploy
cd .. && ./deploy.sh prod
```

## Environments

| Environment | Template | Purpose |
|-------------|----------|---------|
| dev | `.env.dev.example` | Local development on localhost |
| staging | `.env.staging.example` | Pre-production testing on VPS |
| prod | `.env.prod.example` | Production deployment on VPS |

## Required Variables by Environment

### Dev (local)
- `SYNC_BASE_URL` - Usually `http://localhost:8080`

### Staging / Prod (remote)
- `DEPLOY_HOST` - VPS hostname or IP
- `DEPLOY_USER` - SSH user for deployment
- `DEPLOY_PATH` - Remote path (e.g., `/opt/td-sync`)
- `SYNC_BASE_URL` - Public URL of the server

### Prod Only
- `LITESTREAM_S3_BUCKET` - S3 bucket for database backups
- `LITESTREAM_S3_ENDPOINT` - S3 endpoint URL
- `AWS_DEFAULT_REGION` - AWS region
- `AWS_ACCESS_KEY_ID` - AWS credentials
- `AWS_SECRET_ACCESS_KEY` - AWS credentials

## Adding a New Environment

1. Copy an existing template:
   ```bash
   cp .env.staging.example .env.qa.example
   ```

2. Create compose override in `../compose/`:
   ```bash
   cp ../compose/docker-compose.staging.yml ../compose/docker-compose.qa.yml
   ```

3. Create litestream config in `../litestream/`:
   ```bash
   cp ../litestream/litestream.staging.yml ../litestream/litestream.qa.yml
   ```

4. Deploy:
   ```bash
   cp .env.qa.example .env.qa
   # Edit .env.qa with your values
   cd .. && ./deploy.sh qa
   ```

## Security

- **Never commit `.env.*` files** (only `.env.*.example` templates)
- These files are gitignored automatically
- Secrets are copied to the server during deployment
