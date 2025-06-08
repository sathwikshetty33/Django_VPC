package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"sathwikshetty33/Django-vpc/Services"
)

type DeploymentResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	PublicIP  string `json:"public_ip,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

type DeploymentManager struct {
	clients    map[string]map[chan services.LogMessage]bool
	clientsMux sync.RWMutex
	deployments map[string]*DeploymentStatus
	deployMux   sync.RWMutex
}

type DeploymentStatus struct {
	ID        string
	Status    string 
	StartTime time.Time
	EndTime   *time.Time
	Error     error
}

func NewDeploymentManager() *DeploymentManager {
	return &DeploymentManager{
		clients:     make(map[string]map[chan services.LogMessage]bool),
		deployments: make(map[string]*DeploymentStatus),
	}
}

func (dm *DeploymentManager) AddClient(deploymentID string, client chan services.LogMessage) {
	dm.clientsMux.Lock()
	defer dm.clientsMux.Unlock()
	
	if dm.clients[deploymentID] == nil {
		dm.clients[deploymentID] = make(map[chan services.LogMessage]bool)
	}
	dm.clients[deploymentID][client] = true
	
	log.Printf("Client added for deployment %s. Total clients: %d", deploymentID, len(dm.clients[deploymentID]))
}

func (dm *DeploymentManager) RemoveClient(deploymentID string, client chan services.LogMessage) {
	dm.clientsMux.Lock()
	defer dm.clientsMux.Unlock()
	
	if clients, exists := dm.clients[deploymentID]; exists {
		delete(clients, client)
		log.Printf("Client removed for deployment %s. Remaining clients: %d", deploymentID, len(clients))
		
		
	}
	close(client)
}

func (dm *DeploymentManager) BroadcastLog(deploymentID string, logMsg services.LogMessage) {
	dm.clientsMux.RLock()
	clients := dm.clients[deploymentID]
	clientCount := 0
	if clients != nil {
		clientCount = len(clients)
	}
	dm.clientsMux.RUnlock()
	
	log.Printf("Broadcasting log for deployment %s: %s - %s (clients: %d)", deploymentID, logMsg.Level, logMsg.Message, clientCount)
	
	if clients != nil && len(clients) > 0 {
		dm.clientsMux.RLock()
		for client := range clients {
			select {
			case client <- logMsg:
			default:
				log.Printf("Failed to send log to client for deployment %s", deploymentID)
			}
		}
		dm.clientsMux.RUnlock()
		log.Printf("Log broadcasted to %d clients for deployment %s", len(clients), deploymentID)
	} else {
		log.Printf("No active clients for deployment %s - storing log for potential reconnection", deploymentID)
		
	}
}

func (dm *DeploymentManager) SetDeploymentStatus(deploymentID, status string, err error) {
	dm.deployMux.Lock()
	defer dm.deployMux.Unlock()
	
	if deployment, exists := dm.deployments[deploymentID]; exists {
		deployment.Status = status
		deployment.Error = err
		if status == "completed" || status == "failed" {
			now := time.Now()
			deployment.EndTime = &now
		}
	}
}

func (dm *DeploymentManager) GetDeploymentStatus(deploymentID string) *DeploymentStatus {
	dm.deployMux.RLock()
	defer dm.deployMux.RUnlock()
	
	return dm.deployments[deploymentID]
}

func (dm *DeploymentManager) CreateDeployment(deploymentID string) {
	dm.deployMux.Lock()
	defer dm.deployMux.Unlock()
	
	dm.deployments[deploymentID] = &DeploymentStatus{
		ID:        deploymentID,
		Status:    "running",
		StartTime: time.Now(),
	}
}

var deploymentManager = NewDeploymentManager()

func main() {
	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})

	r.POST("/deploy", handleDeployment)
	r.GET("/deploy/:deploymentId/logs", handleLogStream)
	r.GET("/deploy/:deploymentId/status", handleDeploymentStatus)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339)})
	})

	log.Println("Starting server on :8080...")
	r.Run(":8080")
}

func handleDeployment(c *gin.Context) {
	var req services.DeploymentRequest
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, DeploymentResponse{
			Success:   false,
			Error:     fmt.Sprintf("Invalid request body: %v", err),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	if err := validateRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, DeploymentResponse{
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	deploymentID := fmt.Sprintf("%s-%s-%d", req.Username, time.Now().Format("20060102-150405"), time.Now().Unix())
	
	log.Printf("Starting deployment with ID: %s", deploymentID)
	
	deploymentManager.CreateDeployment(deploymentID)
	
	go func() {
		logFunc := func(level, message, step string) {
			logMsg := services.LogMessage{
				Level:     level,
				Message:   message,
				Timestamp: time.Now().Format(time.RFC3339),
				Step:      step,
			}
			
			log.Printf("[%s] %s: %s", level, step, message)
			
			deploymentManager.BroadcastLog(deploymentID, logMsg)
		}
		
		logFunc("info", "Starting deployment...", "initialization")
		
		deploymentService := services.NewDeploymentService()
		
		deploymentManager.SetDeploymentStatus(deploymentID, "running", nil)
		
		publicIP, err := deploymentService.Deploy(&req, deploymentID, deploymentManager)
		
		if err != nil {
			logFunc("error", fmt.Sprintf("Deployment failed: %v", err), "error")
			deploymentManager.SetDeploymentStatus(deploymentID, "failed", err)
		} else {
			logFunc("success", fmt.Sprintf("Deployment completed successfully! Public IP: %s", publicIP), "completed")
			deploymentManager.SetDeploymentStatus(deploymentID, "completed", nil)
		}
		
		deploymentManager.BroadcastLog(deploymentID, services.LogMessage{
			Level:     "system",
			Message:   "DEPLOYMENT_COMPLETE",
			Timestamp: time.Now().Format(time.RFC3339),
			Step:      "system",
		})
		
		log.Printf("Deployment %s completed", deploymentID)
		
		time.Sleep(2 * time.Second)
		deploymentManager.clientsMux.Lock()
		delete(deploymentManager.clients, deploymentID)
		deploymentManager.clientsMux.Unlock()
	}()

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "Deployment started",
		"deployment_id": deploymentID,
		"timestamp":     time.Now().Format(time.RFC3339),
	})
}

func handleLogStream(c *gin.Context) {
	deploymentID := c.Param("deploymentId")
	
	log.Printf("New SSE connection for deployment: %s", deploymentID)
	
	status := deploymentManager.GetDeploymentStatus(deploymentID)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}
	
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "Cache-Control")

	clientChan := make(chan services.LogMessage, 100)
	
	deploymentManager.AddClient(deploymentID, clientChan)
	defer deploymentManager.RemoveClient(deploymentID, clientChan)

	initialMsg := services.LogMessage{
		Level:     "system",
		Message:   fmt.Sprintf("Connected to log stream for deployment %s (status: %s)", deploymentID, status.Status),
		Timestamp: time.Now().Format(time.RFC3339),
		Step:      "connection",
	}
	
	data, _ := json.Marshal(initialMsg)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
	
	log.Printf("SSE connection established for deployment: %s", deploymentID)

	if status.Status == "completed" || status.Status == "failed" {
		completionMsg := services.LogMessage{
			Level:     "system",
			Message:   "DEPLOYMENT_COMPLETE",
			Timestamp: time.Now().Format(time.RFC3339),
			Step:      "system",
		}
		data, _ := json.Marshal(completionMsg)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
		return
	}

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	connectionTimeout := time.NewTimer(10 * time.Minute)
	defer connectionTimeout.Stop()

	for {
		select {
		case logMsg, ok := <-clientChan:
			if !ok {
				log.Printf("Client channel closed for deployment: %s", deploymentID)
				return
			}
			
			connectionTimeout.Reset(10 * time.Minute)
			
			data, err := json.Marshal(logMsg)
			if err != nil {
				log.Printf("Error marshaling log message: %v", err)
				continue
			}
			
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
			
			log.Printf("Sent log to client for deployment %s: %s", deploymentID, logMsg.Message)
			
			if logMsg.Level == "system" && logMsg.Message == "DEPLOYMENT_COMPLETE" {
				log.Printf("Deployment complete, closing SSE connection: %s", deploymentID)
				time.Sleep(1 * time.Second) 
				return
			}
			
		case <-heartbeatTicker.C:
			heartbeat := services.LogMessage{
				Level:     "system",
				Message:   "heartbeat",
				Timestamp: time.Now().Format(time.RFC3339),
				Step:      "heartbeat",
			}
			data, _ := json.Marshal(heartbeat)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
			
		case <-connectionTimeout.C:
			log.Printf("Connection timeout for deployment: %s", deploymentID)
			return
			
		case <-c.Request.Context().Done():
			log.Printf("Client disconnected from deployment: %s", deploymentID)
			return
		}
	}
}

func handleDeploymentStatus(c *gin.Context) {
	deploymentID := c.Param("deploymentId")
	
	status := deploymentManager.GetDeploymentStatus(deploymentID)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}
	
	response := gin.H{
		"deployment_id": status.ID,
		"status":        status.Status,
		"start_time":    status.StartTime.Format(time.RFC3339),
	}
	
	if status.EndTime != nil {
		response["end_time"] = status.EndTime.Format(time.RFC3339)
		response["duration"] = status.EndTime.Sub(status.StartTime).String()
	}
	
	if status.Error != nil {
		response["error"] = status.Error.Error()
	}
	
	c.JSON(http.StatusOK, response)
}

func validateRequest(req *services.DeploymentRequest) error {
	if req.Username == "" {
		return fmt.Errorf("username is required")
	}
	if req.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	if req.GithubToken == "" {
		return fmt.Errorf("github_token is required")
	}

	return nil
}