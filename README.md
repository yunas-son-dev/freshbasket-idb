# FreshBasket Incident Dashboard

A cloud-native incident management web application for a fictional grocery delivery startup, built with Go and deployed on AWS Elastic Beanstalk with a fully managed infrastructure stack.

Operations staff can log, view, and update service incidents through a lightweight web interface. The application is intentionally simple in scope — the focus of this project is the AWS infrastructure design, not the application layer.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Application | Go 1.21 (`net/http`, `html/template`) |
| Database | Amazon RDS MySQL 8.4 |
| Platform | AWS Elastic Beanstalk (Go 1.x managed platform) |
| OS / AMI | Amazon Linux 2023 (custom AMI: `freshbasket-ami`) |
| Networking | Custom VPC (`freshbasket-vpc`, 10.0.0.0/16) |
| Load Balancer | Application Load Balancer (ALB) |
| Scaling | Auto Scaling Group (min 2, max 8 instances) |
| Notifications | Amazon SNS (email alerts) |
| Monitoring | Amazon CloudWatch (CPU utilisation metrics + alarm) |

---

## Infrastructure Design

### High Availability

The application is deployed across two Availability Zones (`us-east-1a`, `us-east-1b`). The ALB distributes traffic across healthy instances in both AZs, so a single AZ failure does not take the service down. RDS Multi-AZ mirrors the primary database to a standby instance in a separate AZ, with automatic failover if the primary becomes unavailable.

### Elastic Scaling

The Auto Scaling Group maintains a minimum of 2 EC2 instances at all times and scales out to a maximum of 8 based on CPU utilisation. Scale-out is triggered when average CPU exceeds 60% for 5 minutes; scale-in when it drops below 30% for 5 minutes. The ALB automatically registers and deregisters instances as the group scales.

### Fault Tolerance

- ALB health checks continuously monitor each EC2 instance and remove unhealthy instances from the rotation.
- ASG automatically replaces any instance that fails a health check, launching a replacement from the custom AMI.
- RDS Multi-AZ provides automatic database failover without manual intervention.
- A minimum instance count of 2 ensures the application remains available even during a rolling update or an instance replacement event.

### Operational Visibility

- Amazon CloudWatch collects CPU utilisation metrics from all EC2 instances in the ASG. These metrics feed the ASG scaling policies.
- A dedicated CloudWatch alarm (`freshbasket-cpu-high`) fires when average CPU exceeds **80% for 5 consecutive minutes**, triggering an SNS email notification directly — independent of Elastic Beanstalk events.
- The alarm also sends an **OK notification** when CPU drops back below the threshold, completing a full monitoring-to-alert loop that mirrors real on-call setups.
- The alarm is provisioned automatically via `.ebextensions/cloudwatch-alarm.config` using CloudFormation references (`AWSEBAutoScalingGroup`, `AWSEBSNSTopic`), so it is recreated on every environment rebuild without manual setup.
- Amazon SNS is additionally subscribed to EB environment events for deployment, health change, and Auto Scaling notifications.

### Managed Cloud Deployment

AWS Elastic Beanstalk orchestrates the full deployment lifecycle — provisioning EC2 instances, configuring the ALB, managing the ASG, and handling rolling application updates. This eliminates manual infrastructure management while retaining full control over the underlying resources. IAM is configured to satisfy AWS Academy Learner Lab constraints (`LabRole` service role, `LabInstanceProfile` instance profile).

---

## Project Structure

```
freshbasket-idb/
├── main.go                  # Application entry point and HTTP handlers
├── go.mod                   # Go module definition
├── go.sum                   # Dependency checksums
├── Procfile                 # EB process definition (web: ./freshbasket-idb)
├── templates/
│   ├── index.html           # Dashboard view (incident list + charts)
│   └── new.html             # New incident form
├── .ebextensions/
│   ├── db.config            # Elastic Beanstalk configuration notes
│   └── cloudwatch-alarm.config  # CloudWatch CPU alarm → SNS (auto-provisioned)
└── .platform/
    └── hooks/predeploy/
        └── 01_chmod.sh      # Sets executable permission on the binary
```

---

## Environment Variables

The application reads the following environment variables at startup. These are set via Elastic Beanstalk environment properties.

| Variable | Description | Default |
|---|---|---|
| `DB_HOST` | RDS endpoint hostname | `localhost` |
| `DB_PORT` | RDS port | `3306` |
| `DB_USER` | Database username | `admin` |
| `DB_PASSWORD` | Database password | *(empty)* |
| `DB_NAME` | Database name | `freshbasket` |
| `PORT` | HTTP listen port | `5000` |

---

## Database Schema

The application creates the `incidents` table automatically on startup if it does not already exist.

```sql
CREATE TABLE IF NOT EXISTS incidents (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    title       VARCHAR(255) NOT NULL,
    severity    ENUM('low', 'medium', 'high', 'critical') NOT NULL DEFAULT 'low',
    description TEXT,
    status      ENUM('open', 'in_progress', 'resolved') NOT NULL DEFAULT 'open',
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## HTTP Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/` | List all incidents with summary stats and charts |
| GET | `/new` | New incident form |
| POST | `/incidents` | Create a new incident |
| POST | `/incidents/{id}/status` | Update the status of an incident |
| GET | `/health` | Health check endpoint (used by ALB) |

---

## AWS Architecture Overview

```
Users
  │
  ▼
Internet Gateway
  │
  ▼
Application Load Balancer  ──────────────────► Amazon SNS
  │                                             (email alerts)
  ├──── AZ us-east-1a ────┐
  │     Public Subnet      │   Auto Scaling Group (min 2, max 8)
  │     EC2 t3.micro       │   managed by Elastic Beanstalk
  │                        │
  └──── AZ us-east-1b ────┘
              │
              ▼
    Amazon RDS MySQL Multi-AZ
    Primary (AZ-1a) + Standby (AZ-1b)
```

All resources reside within `freshbasket-vpc` (10.0.0.0/16). The RDS instance is not publicly accessible — EC2 instances connect via private IP. The RDS security group (`freshbasket-rds-sg`) restricts inbound port 3306 to the EC2 security group (`freshbasket-ec2-sg`) only.

---

## Local Development

```bash
# Set environment variables
export DB_HOST=localhost
export DB_USER=root
export DB_PASSWORD=yourpassword
export DB_NAME=freshbasket
export PORT=5000

# Run
go run main.go
```

Requires a local MySQL instance with the `freshbasket` database created. The table is created automatically on first run.

---

## Deployment

The application is deployed via Elastic Beanstalk using a zip bundle containing the compiled binary, `Procfile`, `templates/`, `.ebextensions/`, and `.platform/`.

```bash
# Build
go build -o freshbasket-idb .

# Create deployment bundle
zip -r freshbasket.zip freshbasket-idb Procfile templates/ .ebextensions/ .platform/

# Deploy
eb deploy
```
