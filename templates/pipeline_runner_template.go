package templates

// Rename dummy_template to DummyTemplate to export it
const RayActorTemplate = `
import ray
import time
import sys
import os
import importlib
import asyncio

@ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
class {{.Actor}}:
    async def {{.Runner}}(self):
        try:
            directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

            # Get the file name without the extension
            file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

            sys.path.append(directory_path)

            # Dynamically import the module
            pipeline_module = importlib.import_module(file_name)

            # Execute the pipeline function
            getattr(pipeline_module, \"{{.PipelineRunner}}\")()
        except Exception as e:
            print(e)
        finally:
            ray.actor.exit_actor()


# Initialize connection to the Ray head node on the default port.
ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus={{.NumCPUs}}).remote()
`

const RemoteRunnerTemplate = `
import ray
import asyncio

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\", runtime_env={\"RAY_ENABLE_RECORD_ACTOR_TASK_LOGGING\": 1})

async def main():
    try:
        # Get the actor
        actor = ray.get_actor(\"{{.Actor}}\")
        result = await actor.runner.remote()
        print(result)
    except Exception as e:
        print(e)

asyncio.run(main())
`
