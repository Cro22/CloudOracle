# Running against cloud providers

CloudOracle supports four resource sources, selected at runtime with the `CLOUDORACLE_PROVIDER` env var: **synthetic** (default, no cloud account required), **aws**, **gcp**, **azure**. The analyzer, report, and dashboard work identically with all four — they only differ in where the resource inventory comes from.

> **Tested status.** The **synthetic** and **AWS** providers have been exercised end-to-end against a live AWS account during development. The **GCP** and **Azure** providers are implemented against their respective SDKs with the same structure and the code compiles + unit-tests pass, **but they have not been run against live GCP / Azure subscriptions** because I don't have credentials for those clouds at the time of writing. Field-mapping tests use struct literals; the SDK call paths themselves are unverified. If you test either, please open an issue with what you find.

## Synthetic (default, no setup)

No credentials, no network calls — the app generates realistic EC2 / RDS / EBS / Lambda records locally. Ideal for demos, CI, and trying the dashboard in seconds.

```bash
docker compose up --build
docker compose exec app /app/cloudoracle seed --count 120
# open http://localhost:8080
```

Tunables:
- `SYNTHETIC_COUNT` (default `100`) — how many resources to generate per `seed`.
- `SYNTHETIC_ACCOUNT` (default `synthetic-account`) — account ID baked into the records.

The synthetic provider is what 99% of demos use. Everything else in the v1 guide — findings, exports, trend tracking, dashboard — works with synthetic data without any cloud credentials.

## AWS (verified)

**1. IAM user with read-only access.** In the AWS Console → IAM → Users → Create user, attach:
- `ReadOnlyAccess`
- `AWSBillingReadOnlyAccess`

Grab the access key + secret. For least-privilege in production, the minimum set is:

```
ec2:DescribeInstances, ec2:DescribeVolumes
rds:DescribeDBInstances, rds:ListTagsForResource
lambda:ListFunctions, lambda:ListTags
ce:GetCostAndUsage
sts:GetCallerIdentity
```

**2. Configure a local profile.** In `~/.aws/credentials` (or `%USERPROFILE%\.aws\credentials` on Windows):

```ini
[cloudoracle]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
region = us-east-2
```

The profile name `cloudoracle` and region `us-east-2` are the defaults. Override with `AWS_PROFILE=xxx` and `AWS_REGION=eu-west-1` if you use different names.

**3. Run the app on the host** (so it can read `~/.aws/credentials`), pointing at the Postgres container:

```bash
docker compose up -d postgres              # DB only in Docker
export CLOUDORACLE_PROVIDER=aws
go run ./cmd/oracle seed                   # fetches real EC2/RDS/EBS/Lambda, upserts, snapshots
go run ./cmd/oracle analyze                # runs rules → findings on real data
go run ./cmd/oracle serve --port 8080      # dashboard + API
```

The STS `GetCallerIdentity` call at startup validates credentials immediately — if the profile is misconfigured or keys are expired, you get the error right away instead of halfway through a scan.

**Running inside Docker with AWS creds** (if you want `docker compose up app` against AWS), pass the creds as env vars to the `app` service in `docker-compose.yml`:

```yaml
environment:
  CLOUDORACLE_PROVIDER: aws
  AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID}
  AWS_SECRET_ACCESS_KEY: ${AWS_SECRET_ACCESS_KEY}
  AWS_REGION: us-east-2
```

The AWS SDK v2 auto-picks these up without needing a profile file. Recommended only for demos — for prod/CI, use IAM roles via instance metadata or IRSA on EKS, not static keys.

**Cost:** `Describe*` / `List*` calls are free. A full `seed` against a typical account is ~5-10 API calls total.

## GCP (untested against a live account)

> Implemented but not verified against a real GCP project.

Expected flow:

1. Enable APIs on your project: Compute Engine, Cloud SQL Admin, Cloud Functions.
2. Set up Application Default Credentials:
   - Dev: `gcloud auth application-default login`
   - Prod: `GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json`
3. Export `GOOGLE_CLOUD_PROJECT=your-project-id`.

Required IAM roles (least privilege):

```
compute.instances.list, compute.disks.list
cloudsql.instances.list
cloudfunctions.functions.list
```

Then:

```bash
docker compose up -d postgres
export CLOUDORACLE_PROVIDER=gcp
export GOOGLE_CLOUD_PROJECT=your-project-id
go run ./cmd/oracle seed
go run ./cmd/oracle serve --port 8080
```

Since this path hasn't been exercised end-to-end, expect to debug the SDK call mapping on first run.

## Azure (untested against a live account)

> Implemented but not verified against a real Azure subscription.

Expected flow:

1. Export `AZURE_SUBSCRIPTION_ID=<your-subscription-guid>`.
2. Authenticate via one of:
   - Dev: `az login`
   - Service principal: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`
   - Managed Identity (when the app runs on Azure)

The provider uses `DefaultAzureCredential`, which tries all methods in order.

Required RBAC role: `Reader` on the subscription. Production scope:

```
Microsoft.Compute/virtualMachines/read
Microsoft.Compute/disks/read
Microsoft.Sql/servers/read, Microsoft.Sql/servers/databases/read
Microsoft.Web/sites/read
```

Then:

```bash
docker compose up -d postgres
export CLOUDORACLE_PROVIDER=azure
export AZURE_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000
go run ./cmd/oracle seed
go run ./cmd/oracle serve --port 8080
```

Same caveat as GCP: no live-account run has been done, so treat first execution as a validation exercise.

---

For env var reference, see [configuration.md](configuration.md).
