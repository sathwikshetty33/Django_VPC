package providers

import (
	"os"
	"os/exec"
	"text/template"
	"fmt"
	"path/filepath"
	"strings"
)

type AzureProvider struct {
	ResourceGroup string
	Location      string
	VMSize        string
	VMName        string
	Path_         string 
}

const azureTfTemplate = `
provider "azurerm" {
  features {}
  subscription_id = "145296c1-8df7-4169-9ff3-7858d9aeea61"
}

resource "azurerm_resource_group" "example" {
  name     = "{{ .ResourceGroup }}"
  location = "{{ .Location }}"
}

resource "azurerm_virtual_network" "example" {
  name                = "example-network"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

resource "azurerm_subnet" "example" {
  name                 = "example-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.2.0/24"]
}

resource "azurerm_public_ip" "example" {
  name                = "example-public-ip"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  allocation_method   = "Static"
  sku                 = "Basic"
}

resource "azurerm_network_security_group" "example" {
  name                = "example-security-group"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  security_rule {
    name                       = "SSH"
    priority                   = 1001
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }
security_rule {
  name                       = "HTTP"
  priority                   = 1003
  direction                  = "Inbound"
  access                     = "Allow"
  protocol                   = "Tcp"
  source_port_range          = "*"
  destination_port_range     = "80"
  source_address_prefix      = "*"
  destination_address_prefix = "*"
}
  security_rule {
    name                       = "HTTP_8000"
    priority                   = 1002
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "8000"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "example" {
  name                = "example-nic"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.example.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.example.id
  }
}

resource "azurerm_network_interface_security_group_association" "example" {
  network_interface_id      = azurerm_network_interface.example.id
  network_security_group_id = azurerm_network_security_group.example.id
}

resource "azurerm_linux_virtual_machine" "example" {
  name                = "{{ .VMName }}"
  resource_group_name = azurerm_resource_group.example.name
  location            = azurerm_resource_group.example.location
  size                = "{{ .VMSize }}"
  admin_username      = "azureuser"
  network_interface_ids = [azurerm_network_interface.example.id]
  disable_password_authentication = false
  admin_password = "P@ssw0rd1234!"

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
    disk_size_gb         = 30
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }

  # Enable auto-shutdown to save costs (optional)
  tags = {
    Environment = "Development"
    AutoShutdown = "19:00"
  }
}

# Auto-shutdown schedule (helps save costs on free trial)
resource "azurerm_dev_test_global_vm_shutdown_schedule" "example" {
  virtual_machine_id = azurerm_linux_virtual_machine.example.id
  location           = azurerm_resource_group.example.location
  enabled            = true

  daily_recurrence_time = "1900"
  timezone              = "UTC"

  notification_settings {
    enabled = false
  }

  tags = {
    Environment = "Development"
  }
}

output "public_ip" {
  value = azurerm_public_ip.example.ip_address
  depends_on = [azurerm_linux_virtual_machine.example]
}

output "resource_group" {
  value = azurerm_resource_group.example.name
}

output "vm_name" {
  value = azurerm_linux_virtual_machine.example.name
}
`

func (a *AzureProvider) GenerateTerraformConfig(path string) error {
	filePath := filepath.Join(path, "main.tf")
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	tmpl, err := template.New("azure").Parse(azureTfTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(file, a)
}

func (a *AzureProvider) InitTerraform(path string) error {
	cmd := exec.Command("terraform", "init")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *AzureProvider) ApplyTerraform(path string) error {
	cmd := exec.Command("terraform", "apply", "-auto-approve")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *AzureProvider) GetTerraformOutput(path, key string) (string, error) {
	cmd := exec.Command("terraform", "output", "-raw", key)
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *AzureProvider) WriteInventory(path, ip string) error {
	content := fmt.Sprintf(`[azure]
%s ansible_user=azureuser ansible_password=P@ssw0rd1234! ansible_connection=ssh ansible_ssh_common_args='-o StrictHostKeyChecking=no'
`, ip)
	return os.WriteFile(filepath.Join(path, "inventory.ini"), []byte(content), 0644)
}

// Helper function to create a minimal Azure provider instance optimized for free tier
func NewMinimalAzureProvider(resourceGroup, vmName string) *AzureProvider {
	return &AzureProvider{
		ResourceGroup: resourceGroup,
		Location:      "East US",        // Often has better free tier availability
		VMSize:        "Standard_B1ls",  // Smallest burstable VM (0.5 GB RAM, 1 vCPU)
		VMName:        vmName,
	}
}

// Alternative function for slightly more resources if B1ls is not available
func NewAzureProviderB1s(resourceGroup, vmName string) *AzureProvider {
	return &AzureProvider{
		ResourceGroup: resourceGroup,
		Location:      "East US",
		VMSize:        "Standard_B1s",   // Small burstable VM (1 GB RAM, 1 vCPU)
		VMName:        vmName,
	}
}