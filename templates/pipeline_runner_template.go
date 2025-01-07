package templates

// Rename dummy_template to DummyTemplate to export it
const RayActorTemplate = `
import ray
import time
import sys
import os
import importlib
import psutil

@ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
class {{.Actor}}:
    def __init__(self) -> None:
        self.finished = False
        self.period = 180

    def {{.Runner}}(self):
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
            self.finished = True
            ray.actor.exit_actor()

    def monitor(self):
        worker_pid = os.getpid()
        print(f"Monitoring PID {worker_pid}")
        process = psutil.Process(worker_pid)
        while not self.finished:
            memory_used = process.memory_info().rss
            memory_used_mb = memory_used / (1024 ** 2)
            print(f"Task is using {memory_used_mb} MB")
            if memory_used_mb > 1000:
                ray.actor.exit_actor()
            else:
                time.sleep(self.period) 

# Initialize connection to the Ray head node on the default port.
ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus={{.NumCPUs}}).remote()
`

const RemoteRunnerTemplate = `
import ray
import time

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\", runtime_env={\"RAY_ENABLE_RECORD_ACTOR_TASK_LOGGING\": 1})

def main():
    try:
        # Get the actor
        actor = ray.get_actor(\"{{.Actor}}\")

        # Trigger the actor's runner method without waiting for completion
        actor.runner.remote()
        actor.monitor.remote()
    except Exception as e:
        print(e)

if __name__ == \"__main__\":
    main()
`
