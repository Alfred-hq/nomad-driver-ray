package templates

const RemoteRunnerTemplate = `
import ray
import os
import sys
import importlib
import asyncio
import signal
import uvloop

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

async def wait_for_interrupt():
    try:
        await asyncio.Future()  # Await a never-completing future
    except asyncio.CancelledError:
        print(\"Interrupt received, canceling infinite loop...\")

async def main():
    loop = asyncio.get_event_loop()

    # Register signal handlers
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(
            sig, lambda s=sig: asyncio.create_task(shutdown(loop, s))
        )

    # Start both tasks
    directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")
    file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

    sys.path.append(directory_path)

    # Dynamically import the pipeline module
    pipeline_module = importlib.import_module(file_name)

    # Execute the pipeline function directly
    task1 = asyncio.create_task(getattr(pipeline_module, \"{{.PipelineRunner}}\")())
    task2 = asyncio.create_task(wait_for_interrupt())

    try:
        await asyncio.gather(task1, task2)
    except asyncio.CancelledError:
        pass

async def shutdown(loop, signal=None):
    if signal:
        print(f\"Received exit signal {signal.name}...\")

    print(\"Closing database connections\")
    await asyncio.sleep(1.0)

    tasks = [t for t in asyncio.all_tasks() if t is not
                asyncio.current_task()]
    [task.cancel() for task in tasks]

    print(f\"Cancelling {len(tasks)} outstanding tasks\")
    await asyncio.gather(*tasks, return_exceptions=True)
    
    await loop.shutdown_asyncgens()
    loop.stop()

if __name__ == \"__main__\":
    uvloop.install()
    asyncio.run(main())
    
    
`
