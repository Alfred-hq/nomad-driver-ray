package templates

// Rename dummy_template to DummyTemplate to export it
const PipelineRunnerTemplate = `
import ray
import time
import sys
import os
import importlib

@ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
class {{.Actor}}:
    def __init__(self):
        self.is_healthy = False

    def {{.Runner}}(self):
        directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

        # Get the file name without the extension
        file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

        sys.path.append(directory_path)

        # Dynamically import the module
        pipeline_module = importlib.import_module(file_name)

        self.is_healthy = True
        getattr(pipeline_module, \"{{.PipelineRunner}}\")()
        self.is_healthy = False

    def health_check(self):
        return self.is_healthy


# Initialize connection to the Ray head node on the default port.
ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2).remote()

time.sleep(10)

pipeline_runner.{{.Runner}}.remote()
`
