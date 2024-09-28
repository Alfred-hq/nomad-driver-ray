package templates

// Rename dummy_template to DummyTemplate to export it
const RayActorTemplate = `
import ray
import time
import sys
import os
import importlib

@ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
class {{.Actor}}:

    def {{.Runner}}(self):
        directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

        # Get the file name without the extension
        file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

        sys.path.append(directory_path)

        # Dynamically import the module
        pipeline_module = importlib.import_module(file_name)

        getattr(pipeline_module, \"{{.PipelineRunner}}\")()


# Initialize connection to the Ray head node on the default port.
ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus=0.5).remote()
`

const RemoteRunnerTemplate = `
import ray
import time

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\", runtime_env={\"RAY_ENABLE_RECORD_ACTOR_TASK_LOGGING\": 1}) 

actor = ray.get_actor(\"{{.Actor}}\")
actor.runner.remote()
`
