"""Reusable model-driven agent/tool loop with LangGraph checkpoint memory."""
from __future__ import annotations
import json
import uuid
from pathlib import Path
from typing import Protocol
from langchain_core.messages import AIMessage, ToolMessage
from langgraph.graph import StateGraph, MessagesState, START, END


class ExternalCapacityError(RuntimeError):
    """Quota exhaustion is checkpointed for explicit later resume, not retried."""


class AgentState(MessagesState):
    goal: str
    facts: dict
    steps: int
    done: bool
    final_answer: str


class ModelProvider(Protocol):
    def decide(self, goal: str, facts: dict, messages: list, tools: list[dict]) -> dict: ...


class AgentRuntime:
    def __init__(self, provider: ModelProvider, registry: dict, context, directory: Path, max_steps=32, can_finish=None):
        self.provider, self.registry, self.context = provider, registry, context
        self.directory, self.max_steps = directory, max_steps
        self.can_finish=can_finish or (lambda state: True)
        (directory/'calls').mkdir(parents=True,exist_ok=True)

    def event(self, kind, data):
        with (self.directory/'events.jsonl').open('a') as stream:
            stream.write(json.dumps({'event':kind,**data},sort_keys=True)+'\n')

    def agent(self,state):
        if state['steps'] >= self.max_steps:
            raise RuntimeError('Agent step budget exhausted; resume with a larger max_steps after reviewing trace')
        specifications=[{'name':name,'description':tool.description,'parameters':tool.args_schema.model_json_schema()} for name,tool in self.registry.items()]
        answer=self.provider.decide(state['goal'],state['facts'],state['messages'],specifications)
        tool=answer['tool']
        self.event('decision',{'step':state['steps']+1,'decision':answer})
        print(f"[agent {state['steps']+1}] {tool}: {answer['reason']}",flush=True)
        if tool=='finish':
            if not self.can_finish(state):
                return {'messages':[AIMessage(content='Finish rejected by completion policy. Check outstanding goal requirements and use tools to complete them.')], 'steps':state['steps']+1}
            return {'messages':[AIMessage(content=answer['reason'])],'steps':state['steps']+1,'done':True,'final_answer':answer['reason']}
        # The tool request is a persisted LangChain tool-call message. LangGraph
        # checkpoints it before execution, so retries retain the same call ID.
        arguments=json.loads(answer['arguments_json'])
        if not isinstance(arguments,dict): raise ValueError('tool arguments must be a JSON object')
        message=AIMessage(content=answer['reason'],tool_calls=[{'name':tool,'args':arguments,'id':uuid.uuid4().hex}])
        return {'messages':[message],'steps':state['steps']+1}

    def tools(self,state):
        call=state['messages'][-1].tool_calls[0]
        self.context.state=state
        cache=self.directory/'calls'/(call['id']+'.json')
        if cache.exists():
            result=json.loads(cache.read_text())
        else:
            self.event('tool_start',{'id':call['id'],'tool':call['name'],'arguments':call['args']})
            try:
                if call['name'] not in self.registry: raise ValueError('unregistered tool')
                result=self.registry[call['name']].invoke(call['args'])
            except ExternalCapacityError:
                raise
            except Exception as error:
                result={'observation':{'error':str(error)},'facts':state['facts']}
            temporary=cache.with_suffix('.tmp')
            temporary.write_text(json.dumps(result,sort_keys=True)); temporary.replace(cache)
        self.event('tool_result',{'id':call['id'],'tool':call['name'],'result':result})
        return {'facts':result['facts'],'messages':[ToolMessage(content=json.dumps(result['observation'],sort_keys=True),tool_call_id=call['id'],name=call['name'])]}

    def compile(self,checkpointer):
        graph=StateGraph(AgentState)
        graph.add_node('agent',self.agent)
        graph.add_node('tools',self.tools)
        graph.add_edge(START,'agent')
        graph.add_conditional_edges('agent',lambda state:'end' if state.get('done') else 'tools' if getattr(state['messages'][-1],'tool_calls',None) else 'agent',{'end':END,'tools':'tools','agent':'agent'})
        graph.add_edge('tools','agent')
        return graph.compile(checkpointer=checkpointer)
