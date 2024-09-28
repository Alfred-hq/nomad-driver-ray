package templates

const RayServeAPITemplate = `
from fastapi import FastAPI, Request, HTTPException, Query
import subprocess
import ray
import uvicorn

app = FastAPI()

@app.get(\"/api/health\")
async def health_check():
    \"""
    Health check endpoint to ensure the server is running.
    \"""
    return {\"status\": \"ok\"}

@app.post(\"/api/actor-status\")
async def actor_status(request: Request):
    \"""
    Get the status of a specific actor based on the provided actor_id.
    \"""
    if request.headers.get(\"content-type\") != \"application/json\":
        raise HTTPException(status_code=400, detail=\"Request must be JSON\")

    data = await request.json()
    actor_id = data.get(\"actor_id\")

    if not actor_id:
        raise HTTPException(status_code=400, detail=\"No actor_id provided\")
    
    try:
        command = f\"ray list actors --filter 'state=ALIVE' | grep {actor_id}\"
        result = subprocess.run(
            command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
        )

        if result.returncode == 0:
            processed_output = ' '.join(result.stdout.strip().split())
            actor_status = processed_output.split(' ')[3] if len(processed_output.split(' ')) >= 4 else \"Unavailable\"
            return {\"status\": \"success\", \"actor_status\": actor_status}
        else:
            return {\"status\": \"error\", \"error\": result.stderr.strip()}

    except Exception as e:
        raise HTTPException(status_code=500, detail=\"Failed to get actor status\")

@app.post(\"/api/actor-logs\")
async def actor_logs(request: Request):
    \"""
    Retrieve the logs of a specific actor based on the provided actor_id.
    \"""
    if request.headers.get(\"content-type\") != \"application/json\":
        raise HTTPException(status_code=400, detail=\"Request must be JSON\")

    data = await request.json()
    actor_id = data.get(\"actor_id\")

    if not actor_id:
        raise HTTPException(status_code=400, detail=\"No actor_id provided\")

    try:
        command = f\"ray list actors --filter 'state=ALIVE' | grep {actor_id}\"
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
        
@app.delete(\"/api/kill-actor\")
async def kill_actor(actor_id: str = Query(...)):
    \"""
    Kill a specific actor based on the provided actor_id.
    \"""
    if not actor_id:
        raise HTTPException(status_code=400, detail=\"No actor_id provided\")

    try:
        actor = ray.get_actor(name=f\"{actor_id}\", namespace=\"{{.Namespace}}\")
        ray.kill(actor)
        return {\"status\": \"success\", \"detail\": f\"{actor_id} deleted\"}

    except Exception as e:
        raise HTTPException(status_code=500, detail=f\"Failed to kill actor: {str(e)}\")

if __name__ == \"__main__\":
    uvicorn.run(app, host=\"0.0.0.0\", port=8000)
`
