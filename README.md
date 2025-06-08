# ⚡ JanGo

> **Lightning-fast Django deployment to Azure with automated CI/CD setup**

A developer-friendly tool that eliminates the complexity of Django deployment. Built by a Django developer, for Django developers who want to deploy applications quickly without the usual hassles of PythonAnywhere and other complex platforms.

## ✨ Features

- **⚡ One-Click Deployment**: Deploy Django apps to Azure VMs in minutes
- **🔄 Automatic CI/CD**: GitHub Actions pipeline configured automatically via GitHub API
- **🛠️ Infrastructure as Code**: Terraform provisions Azure resources with security best practices
- **🎯 Minimal Configuration**: Just provide repo URL, credentials, and environment variables
- **📊 Real-time Monitoring**: Live deployment logs and status updates via React frontend
- **🔧 Smart Detection**: Automatically detects WSGI vs ASGI applications and configures accordingly
- **🔐 Secure**: SSH key-based authentication and proper security group configuration
- **🚀 Production Ready**: Nginx reverse proxy, Supervisor process management, and rate limiting

## 🎯 Why JanGo?

As Django developers, we've all been there:
- ❌ Spending hours configuring deployment pipelines
- ❌ Wrestling with complex PaaS platforms like PythonAnywhere
- ❌ Manual server setup and configuration
- ❌ Repetitive deployment tasks for every new project

**JanGo solves all of this with a single, streamlined workflow.**

## 🏗️ System Architecture

![JanGo System Architecture](https://drive.google.com/uc?export=view&id=1PVH_krWZmiJRN66XfQROwnyWorsF-ElS)

*Complete system architecture showing the flow from user interaction to deployed Django application*


## 🏗️ Architecture & Workflow

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   React Frontend │◄──►│   Go Backend     │◄──►│   Azure VM      │
│   (Logs & Status)│    │  (Orchestration) │    │  (Ubuntu 22.04) │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                   ┌──────────────────────────┐
                   │    Deployment Pipeline   │
                   │                          │
                   │ 1. Terraform → VM Setup │
                   │ 2. SSH Key Generation   │
                   │ 3. Ansible → App Config │
                   │ 4. GitHub Actions Setup │
                   │ 5. Nginx + Supervisor  │
                   └──────────────────────────┘
```

### Deployment Workflow

1. **Infrastructure Provisioning** (Terraform)
   - Creates Azure Resource Group, VNet, and VM
   - Configures security groups (SSH, HTTP, HTTPS, port 8000)
   - Generates SSH key pair for secure access

2. **Server Configuration** (Ansible)
   - Installs system dependencies (Python3, Nginx, Supervisor, etc.)
   - Clones your repository
   - Creates virtual environment
   - Auto-detects Django project structure

3. **Application Setup**
   - Installs requirements automatically
   - Configures CORS and ALLOWED_HOSTS
   - Runs migrations and collects static files
   - Sets up Gunicorn (WSGI) or Uvicorn (ASGI) workers

4. **Production Services**
   - Configures Nginx as reverse proxy with rate limiting
   - Sets up Supervisor for process management
   - Creates comprehensive logging system

5. **CI/CD Integration**
   - Adds SSH key to GitHub Actions secrets
   - Creates automated deployment workflow
   - Enables push-to-deploy functionality

## 🚀 Quick Start

### Prerequisites

- **Azure CLI** logged in (`az login`)
- **Terraform** installed (v1.0+)
- **Ansible** installed (v2.9+)
- **Go** 1.19+ installed
- **Node.js** 16+ and npm
- **GitHub** personal access token with repo permissions

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/sathwikshetty33/JanGo.git
   cd JanGo
   ```

2. **Configure Azure credentials**
   ```bash
   # Login to Azure CLI
   az login
   
   # Create .env file in /api/ directory
   cd api
   echo "AZURE_SUBSCRIPTION_ID=your-azure-subscription-id" > .env
   cd ..
   ```

3. **Install dependencies**
   ```bash
   # Backend dependencies
   go mod tidy
   
   # Frontend dependencies
   cd frontend
   npm install
   cd ..
   ```

4. **Start the application**
   ```bash
   # Start backend
   go run main.go
   
   # In another terminal, start frontend
   cd frontend
   cd deployment-dashbaord
   npm start
   ```

5. **Access the application**
   Open your browser and navigate to `http://localhost:3000`

## 🎮 Usage

### Deployment Parameters

Fill in the deployment form with the following information:

- **Username**: Your preferred username for the deployment
- **Repository URL**: Your Django project's GitHub repository
- **GitHub Token**: Personal access token with repo and workflow permissions
- **Additional Commands**: Custom setup commands (e.g., `pip install requirements.txt`)
- **Environment Variables**: Key-value pairs for Django settings
- **ASGI Application**: Check if using Django Channels, FastAPI, etc.
- **Auto Deploy**: Enable automatic deployment after setup

### Environment Variables Format

```bash
DEBUG=False
SECRET_KEY=your-secret-key-here
DATABASE_URL=postgresql://user:pass@host:port/db
ALLOWED_HOSTS=yourdomain.com,www.yourdomain.com
```

### Supported Application Types

- **WSGI Applications**: Traditional Django apps (uses Gunicorn)
- **ASGI Applications**: Django Channels, FastAPI (uses Gunicorn + Uvicorn workers)

## 🔧 System Requirements & Dependencies

### Automatically Installed on VM

```bash
# System packages
- python3
- python3-pip
- python3-dev
- python3-venv
- git
- nginx
- supervisor
- build-essential
- libpq-dev
- pkg-config
- default-libmysqlclient-dev

# Python packages (auto-detected)
- gunicorn (for WSGI apps)
- uvicorn (for ASGI apps)
- Your project's requirements.txt
```

### Server Configuration

**Nginx Configuration**
```nginx
# Rate limiting zones
limit_req_zone $binary_remote_addr zone=general:10m rate=30r/m;
limit_req_zone $binary_remote_addr zone=auth:10m rate=5r/m;
limit_req_zone $binary_remote_addr zone=api:10m rate=100r/m;
limit_req_zone $binary_remote_addr zone=static:10m rate=200r/m;
```

**Supervisor Configuration**
```ini
[program:django-server]
command=/home/azureuser/app/start_server.sh
directory={{ django_project_path }}
user=azureuser
autostart=true
autorestart=true
stdout_logfile=/home/azureuser/logs/django-server-stdout.log
stderr_logfile=/home/azureuser/logs/django-server-stderr.log
```

## 🔄 Automated CI/CD

After deployment, your repository will automatically include a GitHub Actions workflow:

```yaml
name: Auto Deploy Django Application
on:
  push:
    branches: [ main, master ]
  pull_request:
    branches: [ main, master ]
    types: [closed]

jobs:
  deploy:
    if: github.event_name == 'push' || (github.event_name == 'pull_request' && github.event.pull_request.merged == true)
    runs-on: ubuntu-latest
    
    steps:
    - name: Deploy to server
      uses: appleboy/ssh-action@v1.0.3
      with:
        host: ${{ secrets.SERVER_IP }}
        username: azureuser
        key: ${{ secrets.SSH_PRIVATE_KEY }}
        script: |
          # Automated deployment script
          # - Stops Django server
          # - Pulls latest changes
          # - Updates dependencies
          # - Runs migrations
          # - Collects static files
          # - Restarts server
```

## 📊 What Gets Deployed

### Azure Resources Created

- ✅ **Resource Group** with your specified name
- ✅ **Virtual Network** with proper subnets
- ✅ **Ubuntu 22.04 LTS VM** with your chosen size
- ✅ **Public IP** with static allocation
- ✅ **Security Groups** (SSH, HTTP, HTTPS, 8000)
- ✅ **Auto-shutdown schedule** (saves costs)

### Application Stack

- ✅ **Nginx** as reverse proxy with rate limiting
- ✅ **Gunicorn/Uvicorn** as WSGI/ASGI server
- ✅ **Supervisor** for process management
- ✅ **Virtual environment** isolation
- ✅ **Comprehensive logging** system
- ✅ **Auto-restart** on failures

## 🛠️ Customization

### Terraform Template

You can customize the Azure infrastructure by editing:
```
/Providers/azureProvider.go
```

The template includes:
- VM size configuration
- Security group rules
- Network settings
- Storage options
- Auto-shutdown policies

### Supported Configurations

- **VM Sizes**: Standard_B1s, Standard_B2s, Standard_D2s_v3, etc.
- **Regions**: East US, West Europe, Southeast Asia, etc.
- **Storage**: Standard_LRS, Premium_LRS
- **Auto-shutdown**: Configurable time and timezone

## ⚠️ Important Notes

### Security & Monitoring

> **⚠️ IMPORTANT**: This tool provisions real Azure resources that incur costs. It is your responsibility to:
> - Monitor your Azure spending and usage
> - Configure appropriate security settings for production use
> - Review and test deployments before using in production
> - Regularly update and patch your deployed applications
> - Monitor application logs and performance

### Best Practices

- **Resource Management**: Use auto-shutdown to control costs
- **Security**: Restrict SSH access to your IP range in production
- **Monitoring**: Set up Azure Monitor and alerts
- **Backups**: Configure regular database backups
- **SSL**: Add SSL certificates for production domains

## 🐛 Troubleshooting

### Common Issues

**Q: Terraform fails with authentication error**
```bash
# Ensure you're logged into Azure CLI
az login
az account show  # Verify correct subscription
```

**Q: Ansible connection timeout**
```bash
# Check if VM is running and SSH is accessible
ssh -i azure_vm_key azureuser@<vm-ip>
```

**Q: Django app not starting**
```bash
# Check supervisor logs
sudo supervisorctl status django-server
sudo tail -f /home/azureuser/logs/django-server-stderr.log
```

**Q: GitHub Actions workflow fails**
```bash
# Verify SSH key is properly added to secrets
# Check if repository has proper permissions
```

### Deployment Logs

Monitor real-time deployment progress through the React frontend:
- Terraform execution logs
- Ansible playbook progress
- Application startup status
- Error messages and troubleshooting hints

## 🤝 Contributing

We welcome contributions! Areas for improvement:

- **Multi-cloud support** (AWS, GCP)
- **Database automation** (PostgreSQL, MySQL setup)
- **SSL certificate** automation
- **Monitoring dashboard** integration
- **Backup automation**

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🔗 Useful Commands

```bash
# Check VM status
az vm show -g <resource-group> -n <vm-name> --show-details

# SSH into deployed VM
ssh -i azure_vm_key azureuser@<public-ip>

# Monitor Django application
sudo supervisorctl status django-server
sudo tail -f /home/azureuser/logs/django-server-stdout.log

# Check Nginx status
sudo systemctl status nginx
sudo nginx -t  # Test configuration
```

---

<div align="center">

**Made with ❤️ by Django developers, for Django developers**

[🚀 Get Started](#quick-start) • [🐛 Report Issues](https://github.com/sathwikshetty33/JanGo/issues) • [💡 Request Features](https://github.com/sathwikshetty33/JanGo/issues)

</div>
