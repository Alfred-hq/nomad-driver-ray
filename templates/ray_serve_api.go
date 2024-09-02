package templates

// Rename dummy_template to DummyTemplate to export it
const RayServeAPITemplate = `
from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import JSONResponse
from ray import serve
import subprocess

app = FastAPI()

@serve.deployment(route_prefix="/api")
@serve.ingress(app)
class RayServeAPI:
    def __init__(self):
        pass

    @app.get("/")
    async def root(self):
        return {"message": "Send a POST request to /execute-script with {'script_path': '/path/to/script.sh', 'params': ['arg1', 'arg2']}."}

    @app.post("/execute-script")
    async def execute_script(self, request: Request):
        if request.headers.get("content-type") != "application/json":
            raise HTTPException(status_code=400, detail="Request must be JSON")

        data = await request.json()
        script_path = data.get("script_path")
        params = data.get("params", [])  # Default to an empty list if params are not provided

        if not script_path:
            raise HTTPException(status_code=400, detail="No script_path provided")

        try:
            # Build the command with parameters
            command = [script_path] + params
            result = subprocess.run(
                command, shell=False,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                return {"status": "success", "output": result.stdout, "error": result.stderr}
            else:
                return {"status": "error", "output": result.stdout, "error": result.stderr, "command": command}

        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Execution failed: {str(e)}")

    @app.post("/execute-bash")
    async def execute_bash(self, request: Request):
        if request.headers.get("content-type") != "application/json":
            raise HTTPException(status_code=400, detail="Request must be JSON")

        data = await request.json()
        command = data.get("command")

        if not command:
            raise HTTPException(status_code=400, detail="No command provided")

        try:
            result = subprocess.run(
                command, shell=True,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                return {"status": "success", "output": result.stdout, "error": result.stderr}
            else:
                return {"status": "error", "output": result.stdout, "error": result.stderr, "command": command}

        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Execution failed: {str(e)}")

    @app.post("/actor-status")
    async def actor_status(self, request: Request):
        if request.headers.get("content-type") != "application/json":
            raise HTTPException(status_code=400, detail="Request must be JSON")

        data = await request.json()
        actor_id = data.get("actor_id")

        if not actor_id:
            raise HTTPException(status_code=400, detail="No actor_id provided")

        try:
            command = f"ray list actors | grep {actor_id} | awk '{{printf \"%s\", $4}}'"
            result = subprocess.run(
                command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                return {"status": "success", "actor_status": result.stdout.strip()}
            else:
                return {"status": "error", "error": result.stderr.strip()}

        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Failed to get actor status: {str(e)}")

# Deploy the model
serve.run(RayServeAPI.bind())
`
