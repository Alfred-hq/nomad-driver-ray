package templates

// Rename dummy_template to DummyTemplate to export it
// const RayActorTemplate = `
// import ray
// import time
// import sys
// import os
// import importlib

// @ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
// class {{.Actor}}:
//     def {{.Runner}}(self):
//         try:
//             directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

//             # Get the file name without the extension
//             file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

//             sys.path.append(directory_path)

//             # Dynamically import the module
//             pipeline_module = importlib.import_module(file_name)

//             # Execute the pipeline function
//             getattr(pipeline_module, \"{{.PipelineRunner}}\")()
//         except Exception as e:
//             print(e)
//         finally:
//             ray.actor.exit_actor()

// # Initialize connection to the Ray head node on the default port.
// ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

// pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus={{.NumCPUs}}).remote()
// `

const RemoteRunnerTemplate = `
import ray
import os
import sys
import importlib
import signal
import time

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")
@ray.remote(num_cpus={{.NumCPUs}})



def main():
    try:

        def handle_termination_signal(signum, frame):
            print("Termination signal received. Exiting immediately!")
            exit(0)

        signal.signal(signal.SIGTERM, handle_termination_signal)
        signal.signal(signal.SIGINT, handle_termination_signal)

        # Simulate a long-running task
        while True:
            print("Working...")
            time.sleep(1)

        
    except Exception as e:
        print(f\"Error running workflow: {e}\")
        ray.shutdown()

ray.get(main.remote())


`
