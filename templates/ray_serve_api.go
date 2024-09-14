package templates

const RayServeAPITemplate = `
from flask import Flask, request, jsonify
import subprocess

app = Flask(__name__)

class {{.ServerName}}:
    def __init__(self):
        pass

    @app.route(\"/api/actor-status\"/, methods=[\"/POST\"/])
    def actor_status():
        if request.content_type != \"application/json\":
            return jsonify({\"detail\": \"Request must be JSON\"}), 400

        data = request.get_json()
        actor_id = data.get(\"actor_id\")

        if not actor_id:
            return jsonify({\"detail\": \"No actor_id provided\"}), 400
    
        try:
            command = f\"ray list actors | grep {actor_id}\"
            result = subprocess.run(
                command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
            )

            if result.returncode == 0:
                processed_output = ' '.join(result.stdout.strip().split())
                actor_status = processed_output.split(' ')[3] if len(processed_output.split(' ')) >= 4 else \"Unavailable\"
                print(actor_status)
                return jsonify({\"status\": \"success\", \"actor_status\": actor_status})
            else:
                return jsonify({\"status\": \"error\", \"error\": result.stderr.strip()}), 500

        except Exception as e:
            return jsonify({\"status\": \"error\", \"detail\": \"Failed to get actor\"}), 500

    @app.route(\"/api/actor-logs\"/, methods=[\"/POST\"/])
    def actor_logs():
        if request.content_type != \"application/json\":
            return jsonify({\"detail\": \"Request must be JSON\"}), 400

        data = request.get_json()
        actor_id = data.get(\"actor_id\")

        if not actor_id:
            return jsonify({\"detail\": \"No actor_id provided\"}), 400

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
                    return jsonify({\"status\": \"success\", \"logs\": result.stdout.strip()})
                else:
                    return jsonify({\"status\": \"error\", \"error\": result.stderr.strip()}), 500

        except Exception as e:
            return jsonify({\"status\": \"error\", \"detail\": \"Failed to get actor logs\"}), 500


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8000)
`
