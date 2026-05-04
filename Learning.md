# IOSSA Backend, DevOps Learning Notes... Section - 1

How to run a Go file:

## Running a Go File

To run a Go file, use the following command:

```bash
go run "file name"
```

### Running the Main File

To run the `main.go` file specifically:

```bash
go run .\cmd\api\main.go
```

## Docker

### Build Locally

```bash
docker build -t iossa .
```

### Run the Container

```bash
docker run -p 8080:8080 iossa
```

### Access the Application

Open your browser and navigate to:

http://localhost:8080


# IOSSA Backend, DevOps Learning Notes... Section - 2

## What I Built So Far

* Go backend service (monolith for now)
* Dockerized application
* Deployed to Cloud Run (GCP)
* Manual deployment pipeline working

---

## Core Concepts Learned

### 1. Containers & Docker

* Docker packages app + dependencies into an image
* Local build:

  ```bash
  docker build -t iossa .
  ```
* Run locally:

  ```bash
  docker run -p 8080:8080 iossa
  ```

---

### 2. Cloud Shell vs Local

* Cloud Shell = temporary Linux VM in GCP
* Has Docker + gcloud pre-installed
* Local Docker images stay inside Cloud Shell (not global)

---

### 3. Artifact Registry (Image Storage)

* Stores Docker images in cloud

* Created using:

  ```bash
  gcloud artifacts repositories create iossa-repo \
    --repository-format=docker \
    --location=asia-south1
  ```

* Structure:

  ```
  project → repo → image
  ```

---

### 4. Docker Image Tagging

* Tag = full address of image in registry

* Example:

  ```
  asia-south1-docker.pkg.dev/PROJECT_ID/iossa-repo/iossa
  ```

* Meaning:

  * region
  * project
  * repo
  * image name

---

### 5. Cloud Build (Important Shift)

Instead of:

```
docker build → docker push
```

Use:

```bash
gcloud builds submit --tag asia-south1-docker.pkg.dev/PROJECT_ID/iossa-repo/iossa
```

What happens:

1. Code uploaded to Cloud Build
2. Docker image built in cloud
3. Image pushed to Artifact Registry

---

### 6. Cloud Run Deployment

Deploy service:

```bash
gcloud run deploy iossa \
  --image asia-south1-docker.pkg.dev/PROJECT_ID/iossa-repo/iossa \
  --region asia-south1 \
  --platform managed \
  --allow-unauthenticated
```

What happens:

* Cloud Run pulls image from registry
* Runs container
* Exposes public URL

---

### 7. Full Manual Pipeline

```
Change code
   ↓
Git push
   ↓
Cloud Shell → git pull
   ↓
Cloud Build builds image
   ↓
Image stored in Artifact Registry
   ↓
Cloud Run deploy
   ↓
Live API updated
```

---

## Testing

### Local

```powershell
Invoke-RestMethod \
  -Uri http://localhost:8080/analyze \
  -Method Post \
  -ContentType "application/json" \
  -Body '{"repo_url":"https://github.com/golang/go"}'
```

### Live

```powershell
Invoke-RestMethod \
  -Uri https://<cloud-run-url>/analyze \
  -Method Post \
  -ContentType "application/json" \
  -Body '{"repo_url":"https://github.com/golang/go"}'
```

---

## Logs & Debugging

View logs:

```bash
gcloud run services logs read iossa --region asia-south1
```

Live logs:

```bash
gcloud run services logs tail iossa --region asia-south1
```

List services:

```bash
gcloud run services list
```

---

## Errors Faced

### 1. Docker Push Failed

Error:

```
connect: connection refused
```

Cause:

* Cloud Shell Docker networking issue

Fix:

* Use Cloud Build instead of manual docker push

---

### 2. Confusion: Registry vs Repo

* Registry = system (Artifact Registry)
* Repo = container inside registry (iossa-repo)

---

### 3. Confusion: Where Docker runs

* Local Docker ≠ Cloud Build Docker
* Cloud Build builds images remotely

---

### 4. Cloud Run Not Updating

Cause:

* Old image still deployed

Fix:

* Rebuild + redeploy

---

## Code Fixes

### Port Handling (Required for Cloud Run)

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}
http.ListenAndServe(":"+port, r)
```

### Health Endpoint

```go
r.Get("/", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("IOSSA API is running 🚀"))
})
```

---

## Key Learnings

* Containers are portable runtime units
* Images must be stored in a registry for cloud use
* Cloud Build replaces manual Docker builds
* Cloud Run runs containers, not code directly
* Deployment = new image + redeploy

---

## Current Status

* Manual deployment working 
* Local + live API tested 
* Understanding of pipeline 

---

## Next Step

* Set up CI/CD using GitHub Actions
* Automate:

  * build
  * push
  * deploy

---
