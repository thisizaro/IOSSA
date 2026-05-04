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
