"""Codex transport adapter; runtime depends only on the ModelProvider protocol."""
import json
import sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[2]/'scripts'))
from codex_propose import propose
from .runtime import ExternalCapacityError


class CodexProvider:
    def __init__(self, binary, model, timeout=120):
        self.binary,self.model,self.timeout=binary,model,timeout

    def decide(self,goal,facts,messages,tools):
        schema={'type':'object','properties':{
            'tool':{'type':'string','enum':[t['name'] for t in tools]+['finish']},
            'arguments_json':{'type':'string'},'reason':{'type':'string','maxLength':1600}},
            'required':['tool','arguments_json','reason'],'additionalProperties':False}
        history=[{'role':message.type,'content':message.content, 'tool_calls':getattr(message,'tool_calls',[])} for message in messages]
        prompt=('You are an autonomous portfolio research agent running inside LangGraph. '
                'Decide the NEXT registered tool call using the goal, state and observations. '
                'You control tool order and arguments; tool results will return on the next turn. '
                'Do not execute native shell/file/web tools. Return only the structured tool request. '
                'arguments_json must encode a JSON object matching the selected tool schema. '
                'Use only the development data and supplied observations. Finish only after replay and report succeed. '
                'Respect remaining candidate budget. Inspect measured tradeoffs before reporting. '
                'For finish use arguments_json="{}". Explain your decision briefly.\n'+json.dumps({'goal':goal,'facts':facts,'memory':history[-24:],'tools':tools},sort_keys=True))
        last=None
        for _ in range(3):
            try: return propose({'task':'framework_turn','prompt':prompt,'output_schema':schema},self.binary,self.model,self.timeout)
            except (RuntimeError,TimeoutError,ValueError,OSError) as error:
                if "usageLimitExceeded" in str(error) or "usage limit" in str(error):raise ExternalCapacityError(str(error))
                last=error
        raise RuntimeError(f'Model unavailable after 3 attempts: {last}')
