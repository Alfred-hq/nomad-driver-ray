package templates

const RayServeAPITemplate = `
from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import JSONResponse
from ray import serve
import subprocess

app = FastAPI()

@serve.deployment(route_prefix=\"/api\")
@serve.ingress(app)
class {{.ServerName}}:
    def __init__(self):
        pass
    @app.post(\"/actor-status\")
    async def actor_status(self, request: Request):
        if request.headers.get(\"content-type\") != \"application/json\":
            raise HTTPException(status_code=400, detail=\"Request must be JSON\")

        data = await request.json()
        actor_id = data.get(\"actor_id\")

        if not actor_id:
            raise HTTPException(status_code=400, detail=\"No actor_id provided\")
    
        try:
            command = f\"ray list actors | grep {actor_id}\"
            result = subprocess.run(
                command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                processed_output = ' '.join(result.stdout.strip().split())
                actor_status = processed_output.split(' ')[3] if len(processed_output.split(' ')) >= 4 else \"Unavailable\"
                print(actor_status)
                return {\"status\": \"success\", \"actor_status\": actor_status}
            else:
                return {\"status\": \"error\", \"error\": result.stderr.strip()}

        except Exception as e:
            raise HTTPException(status_code=500, detail=\"Failed to get actor\")

# Deploy the model
serve.run({{.ServerName}}.bind())
`
