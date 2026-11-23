import json
from pathlib import Path
import tempfile
import unittest
from langchain_core.tools import tool
from langchain_core.messages import HumanMessage
from langgraph.checkpoint.sqlite import SqliteSaver
from portfolio_agent.runtime import AgentRuntime


class FakeProvider:
    def __init__(self,decisions):self.decisions=iter(decisions);self.observations=[]
    def decide(self,goal,facts,messages,tools):
        self.observations.append([m.content for m in messages])
        name,args=next(self.decisions)
        return {'tool':name,'arguments_json':json.dumps(args),'reason':'test decision'}


class RuntimeTests(unittest.TestCase):
    def test_checkpoint_resume_does_not_repeat_completed_tool(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory=Path(temporary)
            class Context:pass
            context=Context();calls=[]
            @tool
            def calculate(value:int):
                """Record a successful bounded computation."""
                calls.append(value)
                return {'facts':{'remaining_budget':0,'replayed':True,'report':True},'observation':{'answer':value}}
            config={'configurable':{'thread_id':'test'}}
            first=FakeProvider([('calculate',{'value':3})])
            with SqliteSaver.from_conn_string(str(directory/'state.sqlite')) as saver:
                graph=AgentRuntime(first,{'calculate':calculate},context,directory).compile(saver)
                stream=graph.stream({'goal':'test','facts':{},'steps':0,'done':False,'messages':[HumanMessage(content='test')]},config,stream_mode='updates',durability='sync')
                for update in stream:
                    if 'tools' in update:break
                stream.close()
            second=FakeProvider([('finish',{})])
            with SqliteSaver.from_conn_string(str(directory/'state.sqlite')) as saver:
                graph=AgentRuntime(second,{'calculate':calculate},context,directory).compile(saver)
                state=graph.invoke(None,config)
            self.assertTrue(state['done'])
            self.assertEqual(calls,[3])
            self.assertTrue(any('answer' in str(x) for x in second.observations))

    def test_tool_failure_is_observed_and_agent_can_recover(self):
        with tempfile.TemporaryDirectory() as temporary:
            class Context:pass
            context=Context()
            @tool
            def succeed(value:int):
                """Return a verified result for a valid value."""
                return {'facts':{'remaining_budget':0,'replayed':True,'report':True},'observation':{'result':value}}
            provider=FakeProvider([('unknown',{}),('succeed',{'value':4}),('finish',{})])
            with SqliteSaver.from_conn_string(str(Path(temporary)/'state.sqlite')) as saver:
                graph=AgentRuntime(provider,{'succeed':succeed},context,Path(temporary)).compile(saver)
                state=graph.invoke({'goal':'test','facts':{},'steps':0,'done':False,'messages':[HumanMessage(content='test')]},{'configurable':{'thread_id':'test'}})
            self.assertTrue(state['done'])
            self.assertTrue(any('unregistered tool' in str(x) for x in provider.observations))

    def test_finish_cannot_bypass_evidence_gates(self):
        with tempfile.TemporaryDirectory() as temporary:
            class Context:pass
            provider=FakeProvider([('finish',{})])
            with SqliteSaver.from_conn_string(str(Path(temporary)/'state.sqlite')) as saver:
                graph=AgentRuntime(provider,{},Context(),Path(temporary),max_steps=1,can_finish=lambda state:False).compile(saver)
                with self.assertRaisesRegex(RuntimeError,'step budget'):
                    graph.invoke({'goal':'test','facts':{'remaining_budget':5},'steps':0,'done':False,'messages':[HumanMessage(content='test')]},{'configurable':{'thread_id':'test'}})


if __name__=='__main__':unittest.main()
