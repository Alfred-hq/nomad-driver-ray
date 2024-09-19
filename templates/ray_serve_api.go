package templates

const RayServeAPITemplate = `
from fastapi import FastAPI, Request, HTTPException, Query
from fastapi.responses import JSONResponse
from ray import serve
import subprocess
import ray

serve.shutdown()
app = FastAPI()
serve.start(detached=True, http_options={\"host\": \"0.0.0.0\", \"port\": 8000})

@serve.deployment(route_prefix=\"/api\")
@serve.ingress(app)
class {{.ServerName}}:
    def __init__(self):
        pass

    @app.get(\"/health\")
    async def actor_status(self, request: Request):
        return {\"status\": \"ok\"}

       
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

    @app.post(\"/actor-logs\")
    async def actor_logs(self, request: Request):
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
                id = processed_output.split(' ')[1] if len(processed_output.split(' ')) >= 4 else \"Unavailable\"

            command = f\"ray logs actor --id {id} --tail 100\"
            result = subprocess.run(
                command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                return {\"status\": \"success\", \"logs\": result.stdout.strip()}
            else:
                return {\"status\": \"error\", \"error\": result.stderr.strip()}

        except Exception as e:
            raise HTTPException(status_code=500, detail=\"Failed to get actor logs\")
            
    @app.delete(\"/kill-actor\")
    async def kill_actor(self, actor_id: str = Query(...)):
        if not actor_id:
            raise HTTPException(status_code=400, detail=\"No actor_id provided\")

        try:
            actor = ray.get_actor(name=f\"{actor_id}\", namespace=\"{{.Namespace}}\")
            ray.kill(actor)
            return {\"status\": \"success\"}

        except Exception as e:
            raise HTTPException(status_code=500, detail=f\"Failed to kill actor: {str(e)}\")

# Deploy the model
serve.run({{.ServerName}}.bind())
`
