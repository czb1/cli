#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Generate an OpenAPI 2.0 (Swagger) document for OMResTool from the API doc.

The CLI treats swagger.json as the single source of truth. Because the OMResTool
backend does not (yet) emit a swagger.json, we derive one here from the documented
API. If the backend later produces a real swagger.json via swag/springdoc, drop it
into internal/cli/docs/swagger.json and this generator becomes unnecessary.

Paths use the FRONTEND paths (with /api, /sbbapi, /list, /longtime prefixes),
because the base URL https://omtool.rnd.huawei.com is the host that exposes those routes
through the Vue proxy. If you instead point the CLI directly at the backend
(prefixes stripped), regenerate with STRIP_PREFIX=1.
"""
import json
import os

STRIP_PREFIX = os.environ.get("STRIP_PREFIX", "0") == "1"
PREFIXES = ("/api", "/sbbapi", "/list", "/longtime", "/myapi")


def path_of(p):
    if not STRIP_PREFIX:
        return p
    for pre in PREFIXES:
        if p.startswith(pre + "/"):
            return p[len(pre):]
    return p


# Common response schema: {code, msg, data?}
def resp(data_schema=None):
    props = {
        "code": {"type": "integer", "description": "状态码，0=成功"},
        "msg": {"type": "string", "description": "提示信息"},
    }
    if data_schema is not None:
        props["data"] = data_schema
    return {
        "200": {
            "description": "OK",
            "schema": {"type": "object", "properties": props},
        }
    }


DATA_OBJ = {"type": "object", "description": "返回数据"}
DATA_ARR = {"type": "array", "items": {"type": "object"}, "description": "返回列表"}


def raw_resp(schema):
    """Response override for endpoints that do NOT use the {code,msg,data} shape.

    性能指标相关接口返回的是 {status, data, message}，用 code=0 判成功会误判，
    所以这些接口直接给出自己的响应 schema。
    """
    return {"200": {"description": "OK", "schema": schema}}


def status_resp(data_desc, message_desc):
    """{"status":true,"data":...,"message":...} 形式的响应。"""
    return raw_resp({
        "type": "object",
        "properties": {
            "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
            "data": data_desc,
            "message": {"type": "string", "description": message_desc},
        },
    })


def body(props, required):
    """props: list of (name, type, desc). required: list of names."""
    schema_props = {}
    for name, typ, desc in props:
        if isinstance(typ, dict):
            node = dict(typ)
            node.setdefault("description", desc)
        else:
            node = {"type": typ, "description": desc}
        schema_props[name] = node
    schema = {"type": "object", "properties": schema_props}
    if required:
        schema["required"] = required
    return [{
        "name": "body",
        "in": "body",
        "required": True,
        "schema": schema,
    }]


def arr_body(item_obj, desc=""):
    """请求体是一个「记录数组」（删除类接口常见）。"""
    return [{
        "name": "body",
        "in": "body",
        "required": True,
        "description": desc,
        "schema": {"type": "array", "items": item_obj, "description": desc},
    }]


def obj(props, required=None, desc=""):
    schema_props = {}
    for name, typ, d in props:
        schema_props[name] = {"type": typ, "description": d} if not isinstance(typ, dict) else typ
    node = {"type": "object", "description": desc, "properties": schema_props}
    if required:
        node["required"] = required
    return node


def arr_of(item_obj, desc=""):
    return {"type": "array", "items": item_obj, "description": desc}


def path_param(name, typ, desc):
    return {"name": name, "in": "path", "required": True, "type": typ, "description": desc}


def query_param(name, typ, required, desc):
    return {"name": name, "in": "query", "required": required, "type": typ, "description": desc}


def form_param(name, typ, required, desc):
    return {"name": name, "in": "formData", "required": required, "type": typ, "description": desc}


paths = {}


def add(p, method, summary, params, tags, data=None, consumes=None, produces=None,
        description=None, responses=None):
    entry = {"summary": summary}
    if description:
        entry["description"] = description
    entry["tags"] = tags
    entry["parameters"] = params
    entry["responses"] = responses if responses is not None else resp(data)
    if consumes:
        entry["consumes"] = consumes
    if produces:
        entry["produces"] = produces
    paths.setdefault(path_of(p), {})[method.lower()] = entry


# ---------------------------------------------------------------------------
# 1. 用户登录
add("/api/auth/login", "post", "域账号登录",
    body([("userName", "string", "用户名（域账号）"),
          ("passwd", "string", "密码")], ["userName", "passwd"]),
    ["Auth"], data=DATA_OBJ)

# 2. 新建工程
add("/api/task/create", "post", "新建工程/任务",
    body([("taskName", "string", "任务名称"),
          ("inputResName", "string", "输入资源名称"),
          ("creator", "string", "创建人"),
          ("neType", "string", "网元类型，如 UNC"),
          ("productType", "string", "产品类型，如 \"0\""),
          ("version", "string", "版本号"),
          ("buildVersion", "integer", "构建版本"),
          ("selectedServices", "string", "选中服务"),
          ("gitBranch", "string", "Git分支")], ["taskName"]),
    ["Task"],
    responses=raw_resp({
        "type": "object",
        "properties": {
            "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
            "extendData": {"type": "integer", "description": "新建工程/任务的 taskId"},
        },
    }))

# 3. 上传文件 (multipart/form-data)
add("/api/upload/uploadFile", "post", "上传并解压zip/rar文件",
    [form_param("taskId", "string", True, "任务ID"),
     form_param("file", "file", True, "上传的文件（zip/rar）")],
    ["Upload"], consumes=["multipart/form-data"])

# 4. 解析上传文件
add("/longtime/upload/parseXml", "post", "开始解析解压后的XML文件",
    body([("path", "string", "解压后的文件路径"),
          ("fileName", "string", "文件名"),
          ("taskId", "string", "任务ID"),
          ("flag", "string", '是否覆盖已有数据（"true"/"false"）')],
         ["path", "fileName", "taskId", "flag"]),
    ["Upload"])

# 5. 添加MOC名称
add("/sbbapi/moc/addMocName", "post", "新增MOC名称",
    body([("mocName", "string", "MOC名称"),
          ("moduleId", "string", "模块ID（数字）"),
          ("taskId", "string", "任务ID（数字）"),
          ("mocDescCh", "string", "MOC中文描述"),
          ("mocDescEn", "string", "MOC英文描述"),
          ("mocTypeId", "string", 'MOC类型ID（"1@"表示自动复合配置）'),
          ("isProcessReport", "string", "是否流程报告"),
          ("m2k", "string", "网管可见性"),
          ("maxRecordNum", "string", "最大记录数"),
          ("minRecordNum", "string", "最小记录数"),
          ("recUpgMode", "string", "记录升级模式"),
          ("publicMode", "string", "公共模式（默认INNER_MODE）")],
         ["mocName", "moduleId", "taskId", "mocDescCh", "mocDescEn", "mocTypeId"]),
    ["Moc"])

# 6. 查询MOC名称
add("/sbbapi/moc/selectMocName", "post", "根据任务ID和模块ID查询MOC名称列表",
    body([("taskId", "string", "任务ID（数字）"),
          ("moduleId", "string", "模块ID（数字）")],
         ["taskId", "moduleId"]),
    ["Moc"], data=DATA_ARR)

# 7. 添加自定义数据类型
cdt = obj([
    ("dataType", "string", "数据类型名称"),
    ("moduleId", "integer", "模块ID"),
    ("eaguId", "string", "EAGU ID"),
    ("dataTypeType", "string", "数据类型类型"),
    ("isExtendedEnum", "integer", "是否为扩展枚举"),
    ("belongExtendedEnum", "string", "所属扩展枚举"),
    ("extendedEnumItem", "string", "扩展枚举项"),
], required=["dataType"], desc="自定义数据类型对象")
add("/list/customize/datatype/add", "post", "添加自定义数据类型（枚举/扩展枚举）",
    body([("projectId", "integer", "项目/任务ID"),
          ("cdt", cdt, "自定义数据类型对象")],
         ["projectId", "cdt"]),
    ["Datatype"])

# 8. 添加枚举项
enums_table = obj([
    ("enumItemName", "string", "枚举项名称"),
    ("enumItemValue", "integer", "枚举项值"),
    ("enumDatatypeNameId", "integer", "所属数据类型ID（外键）"),
    ("enumDescCh", "string", "枚举中文描述"),
    ("enumDescEn", "string", "枚举英文描述"),
    ("meaningCh", "string", "中文含义"),
    ("meaningEn", "string", "英文含义"),
    ("isHide", "integer", "是否隐藏"),
    ("isCommon", "integer", "是否通用"),
    ("eaguId", "string", "EAGU ID"),
    ("dataTypeName", "string", "数据类型名称"),
    ("extendEnumIds", "string", "扩展枚举ID列表"),
], required=["enumItemName", "enumDatatypeNameId"], desc="枚举表对象")
add("/list/customize/datatype/enum/add", "post", "为自定义数据类型添加枚举项",
    body([("projectId", "integer", "项目/任务ID"),
          ("moduleId", "integer", "模块ID"),
          ("enumsTable", enums_table, "枚举表对象")],
         ["projectId", "enumsTable"]),
    ["Datatype"])

# 9. 查询所有自定义数据类型
add("/list/customize/datatype/queryAll", "post", "按工程和类型查询所有自定义数据类型",
    body([("projectId", "integer", "项目/任务ID"),
          ("dataTypeType", "string", "数据类型分类（enum/extend）")],
         ["projectId"]),
    ["Datatype"], data=DATA_ARR)

# 10. 添加MOC字段名称
add("/sbbapi/mocField/addMocFieldName", "post", "为MOC添加字段名称",
    body([("mocId", "string", "MOC ID（数字）"),
          ("fieldName", "string", "字段名称"),
          ("taskId", "string", "任务ID（数字）"),
          ("isKey", "string", "是否关键字段"),
          ("isCustomKey", "string", "是否自定义关键字"),
          ("isUnique", "string", "是否唯一"),
          ("isMandatory", "string", "是否必填"),
          ("m2v", "string", "网管字段可见性")],
         ["mocId", "fieldName", "taskId"]),
    ["MocField"])

# 11. 查询MOC字段名称
add("/sbbapi/mocField/selectMocFieldName", "post", "根据任务ID和MOC ID查询字段列表",
    body([("taskId", "string", "任务ID（数字）"),
          ("mocId", "string", "MOC ID（数字）"),
          ("moduleId", "string", "模块ID（数字）")],
         ["taskId", "mocId", "moduleId"]),
    ["MocField"], data=DATA_ARR)

# 12. 更新MOC属性信息
add("/sbbapi/mocField/updateFieldInfo", "post", "更新MOC对应的属性/字段信息",
    body([("mocId", "string", "MOC ID（数字）"),
          ("fieldName", "string", "字段名称"),
          ("taskId", "string", "任务ID（数字）"),
          ("fieldId", "string", "字段ID（数字）"),
          ("fieldDescCh", "string", "字段中文描述"),
          ("fieldDescEN", "string", "字段英文描述"),
          ("dataTypeId", "string", "数据类型ID（数字）"),
          ("isMandatory", "string", "是否必填（默认0）"),
          ("isKey", "string", "是否关键字（默认0）"),
          ("isUnique", "string", "是否唯一（默认0）"),
          ("m2v", "string", "网管可见性（默认1）"),
          ("defaultValue", "string", "默认值"),
          ("invalidValue", "string", "无效值"),
          ("pattern", "string", "正则校验模式"),
          ("range", "string", "范围值"),
          ("customizeDataTypeId", "string", "自定义数据类型ID"),
          ("isCustomKey", "string", "是否自定义关键字"),
          ("isDam", "string", "是否DAM"),
          ("length", "string", "字段长度"),
          ("indexAssignMode", "string", "索引分配模式（MIN/INC/FAR/SEG）"),
          ("isIndexField", "string", "是否索引字段"),
          ("domDamAssociateField", "string", "DOM DAM关联字段"),
          ("isSupportFuzzyQuery", "string", "是否支持模糊查询")],
         ["mocId", "fieldName", "taskId", "fieldId", "fieldDescCh",
          "fieldDescEN", "dataTypeId", "isMandatory"]),
    ["MocField"])

# 13. 添加默认记录
add("/sbbapi/defaultRecord/add", "post", "为MOC添加默认记录",
    body([("mocId", "integer", "MOC ID"),
          ("taskId", "integer", "任务ID"),
          ("defaultRecords", {"type": "object",
                              "description": "默认记录映射，key为字段ID，value为字段值；特殊key: _index、_rowKey"},
           "默认记录映射")],
         ["mocId", "taskId", "defaultRecords"]),
    ["DefaultRecord"])

# 14. 添加MML方法名称
add("/sbbapi/method/addMethodName", "post", "为MOC添加MML方法/命令",
    body([("commandType", "string", "命令类型（Action/Dsp/Add/Lst/Modify/Remove/CreateOrSet）"),
          ("mocId", "string", "MOC ID（数字）"),
          ("taskId", "string", "任务ID（数字）"),
          ("mocTypeId", "string", "MOC类型ID（数字）")],
         ["commandType", "mocId", "taskId", "mocTypeId"]),
    ["Method"])

# 15. 更新MML方法名称
add("/sbbapi/method/updateMethodName", "post", "更新MML方法/命令信息",
    body([("taskId", "string", "任务ID（数字）"),
          ("methodId", "string", "方法/命令ID（数字）"),
          ("commandType", "string", "命令类型"),
          ("mmlCommandName", "string", "MML命令名称"),
          ("moduleName", "string", "模块名称")],
         ["taskId", "methodId", "commandType", "mmlCommandName"]),
    ["Method"])

# 16. 删除MML方法名称
add("/sbbapi/method/deleteMethodName", "post", "删除MML方法/命令",
    body([("methodIds", "string", "方法ID列表，逗号分隔"),
          ("taskId", "string", "任务ID（数字）"),
          ("moduleName", "string", "模块名称")],
         ["methodIds", "taskId", "moduleName"]),
    ["Method"])

# 17. 查询MML方法信息
add("/sbbapi/method/selectMethodInfo", "post", "查询MOC关联的MML方法信息",
    body([("taskId", "string", "任务ID（数字）"),
          ("mocId", "string", "MOC ID（数字）")],
         ["taskId", "mocId"]),
    ["Method"], data=DATA_ARR)

# 18. 新增或修改MML命令
mml_command_table = obj([
    ("id", "integer", "主键（修改时传入）"),
    ("mmlCommandName", "string", "命令名称"),
    ("commandType", "string", "命令类型（action/get/create/get-config/update/delete/createorset）"),
    ("mocId", "integer", "MOC ID"),
    ("mocName", "string", "MOC名称"),
    ("descCh", "string", "命令中文描述"),
    ("descEn", "string", "命令英文描述"),
    ("meaningCh", "string", "命令功能中文"),
    ("meaningEn", "string", "命令功能英文"),
    ("effectCh", "string", "影响中文"),
    ("effectEn", "string", "影响英文"),
    ("hintCh", "string", "高风险提示中文"),
    ("hintEn", "string", "高风险提示英文"),
    ("hintWarningCh", "string", "高风险影响中文"),
    ("hintWarningEn", "string", "高风险影响英文"),
    ("hintWorkaroundCh", "string", "规避措施中文"),
    ("hintWorkaroundEn", "string", "规避措施英文"),
    ("warningCh", "string", "注意事项中文"),
    ("warningEn", "string", "注意事项英文"),
    ("cmdExampleCh", "string", "命令示例中文"),
    ("cmdExampleEn", "string", "命令示例英文"),
    ("useExampleCh", "string", "使用示例中文"),
    ("useExampleEn", "string", "使用示例英文"),
    ("client", "string", "客户端"),
    ("service", "string", "服务"),
    ("uri", "string", "URI"),
    ("sendType", "string", "发送类型"),
    ("localGroup", "string", "本地组"),
    ("userGroup", "string", "网管权限/用户组"),
    ("preParam", "string", "前置参数"),
    ("commandProcess", "string", "命令流程"),
    ("mmlReport", "string", "MML报告"),
    ("mmlCommandId", "integer", "MML命令ID（远端）"),
    ("max", "integer", "最大值"),
    ("maxRecords", "integer", "最大记录数"),
    ("isHint", "string", "是否为提示命令"),
    ("hintGrade", "string", "风险等级"),
    ("isMultiCast", "integer", "是否多播"),
    ("isCustom", "integer", "是否自定义"),
    ("isMmlAuth", "integer", "是否需要二次授权"),
    ("authTipsCh", "string", "二次授权中文提示"),
    ("authTipsEn", "string", "二次授权英文提示"),
    ("bypassStatus", "string", "是否命令逃逸（白名单）YES/NO"),
    ("bypassAffectCh", "string", "白名单影响中文"),
    ("bypassAffectEn", "string", "白名单影响英文"),
    ("centralConfig", "integer", "是否集中配置命令"),
    ("upgradeBlock", "integer", "是否升级阻塞命令"),
    ("isSupportPartSuccess", "integer", "是否支持部分成功"),
    ("isSupportRestExec", "integer", "是否支持REST执行"),
    ("isSupportMTCenter", "integer", "是否支持MT Center"),
    ("tableDescCh", "string", "表描述中文"),
    ("tableDescEn", "string", "表描述英文"),
    ("referenceCh", "string", "参考中文"),
    ("referenceEn", "string", "参考英文"),
    ("eaguId", "string", "EAGU ID"),
    ("moduleId", "integer", "模块ID"),
    ("graphicId", "string", "图形ID"),
    ("graphicEn", "string", "图形英文标注"),
    ("graphicCh", "string", "图形中文标注"),
    ("outputItemDescriptionCh", "string", "输出项描述中文"),
    ("outputItemDescriptionEn", "string", "输出项描述英文"),
    ("defaultValueQueryCmd", "string", "默认值查询命令"),
    ("commandOperationLogType", "string", "命令操作日志类型"),
    ("isHasGraphic", "string", "是否有图形"),
    ("serviceTypeIsMust", "integer", "服务类型是否必填"),
    ("serviceInstanceIsMust", "integer", "服务实例是否必填"),
    ("innerCommandType", "integer", "内部命令类型"),
], required=["mmlCommandName", "commandType"], desc="MML命令对象")
add("/api/mmlCommand/insertOrUpdate", "post", "新增或修改MML命令",
    body([("taskId", "integer", "任务ID"),
          ("mocTypeId", "integer", "MOC类型ID"),
          ("hotBackupChkCmd", "integer", "热备检查命令标志"),
          ("hotBackupChkCmdId", "integer", "热备检查命令ID"),
          ("mmlCommandTable", mml_command_table, "MML命令对象")],
         ["taskId", "mocTypeId", "mmlCommandTable"]),
    ["MmlCommand"])

# 19. 新增或修改命令参数
command_para_table = obj([
    ("id", "integer", "主键（修改时传入）"),
    ("commandId", "integer", "命令ID（外键）"),
    ("name", "string", "参数名称"),
    ("type", "string", "参数类型（输入/输出）"),
    ("isMust", "string", "是否必填"),
    ("isBranch", "string", "是否分支参数"),
    ("isCommonPara", "integer", "是否通用参数"),
    ("isHidePara", "integer", "是否隐藏参数"),
    ("isUsedtoMeManager", "string", "是否用于ME管理器"),
    ("defaultValue", "string", "默认值"),
    ("defaultPromptCh", "string", "默认提示中文"),
    ("defaultPromptEn", "string", "默认提示英文"),
    ("configCh", "string", "配置中文"),
    ("configEn", "string", "配置英文"),
    ("hint", "integer", "提示"),
    ("otherAttribute", "string", "其他属性"),
    ("resultTableId", "string", "结果表ID"),
    ("resulttableOperation", "string", "结果表操作"),
    ("typeRestrictmap", "string", "类型限制映射（输入）"),
    ("typeRestrictmapOut", "string", "类型限制映射（输出）"),
    ("range", "string", "范围"),
    ("customParaRange", "string", "自定义参数范围"),
    ("mmlParaId", "integer", "MML参数ID"),
    ("mmlParaName", "string", "MML参数名称"),
    ("mocId", "integer", "MOC ID"),
    ("eaguId", "string", "EAGU ID"),
    ("customizeDataTypeId", "integer", "自定义数据类型ID"),
    ("mmlDataTypeId", "integer", "MML数据类型ID"),
    ("fieldDataTypeId", "integer", "对象属性ID/字段数据类型ID"),
    ("fieldId", "integer", "字段ID"),
    ("hasDefaultTableId", "integer", "是否有默认表ID"),
    ("hasMultiTableId", "integer", "是否有多表ID"),
    ("typeInWeb", "string", "Web端类型"),
    ("inputParamOrder", "integer", "输入参数顺序"),
    ("outputParamOrder", "integer", "输出参数顺序"),
    ("condition1", "string", "条件"),
    ("associateMocId", "integer", "关联MOC ID"),
    ("associateMocName", "string", "关联MOC名称"),
    ("associateFieldId", "integer", "关联字段ID"),
    ("associateFieldName", "string", "关联字段名称"),
    ("customedAssociateMocName", "string", "自定义关联MOC名称"),
    ("customedAssociateFieldName", "string", "自定义关联字段名称"),
    ("associateInfo", "string", "关联信息"),
    ("extendEnumValue", "string", "扩展枚举值"),
    ("isCustom", "integer", "是否自定义"),
    ("tableDescCh", "string", "表描述中文"),
    ("tableDescEn", "string", "表描述英文"),
], required=["name"], desc="命令参数对象")
add("/api/commandPara/insertOrUpdate", "post", "新增或修改命令参数",
    body([("taskId", "integer", "任务ID"),
          ("commandId", "integer", "命令ID"),
          ("mocName", "string", "MOC名称"),
          ("mmlParaName", "string", "MML参数名称"),
          ("mmlDataType", "integer", "MML数据类型"),
          ("fieldId", "integer", "字段ID"),
          ("commandParaTable", command_para_table, "命令参数对象")],
         ["taskId", "commandId", "commandParaTable"]),
    ["CommandPara"])

# 20. 根据ID查询MML命令
add("/api/mmlCommand/selectByIdMmlCommand", "post", "根据ID查询MML命令详情",
    body([("id", "integer", "MML命令ID")], ["id"]),
    ["MmlCommand"], data=DATA_OBJ)

# 21. 查询MML参数列表（新增参数时）
add("/api/mmlPara/list1", "post", "查询MML参数列表，用于新增参数场景",
    body([("commandId", "integer", "命令ID"),
          ("mocId", "integer", "MOC ID")], ["commandId"]),
    ["MmlPara"], data=DATA_ARR)

# 22. 查询命令参数列表
add("/api/commandPara/list", "post", "查询命令参数列表",
    body([("commandId", "integer", "命令ID")], ["commandId"]),
    ["CommandPara"], data=DATA_ARR)

# 23. 新增或修改命令分支
child_para = obj([
    ("childCommandParaId", "integer", "子命令参数ID"),
    ("isChildMustGive", "integer", "子参数是否必填"),
    ("childCommandParaName", "string", "子命令参数名称"),
    ("childCommandExtendParaName", "string", "子命令扩展参数名称"),
], desc="子命令参数")
add("/api/commandBranch/insertOrUpdate", "post", "新增或修改命令分支",
    body([("id", "integer", "主键（修改时传入）"),
          ("commandId", "integer", "命令ID"),
          ("taskId", "integer", "任务ID"),
          ("switchCommandParaId", "integer", "开关参数ID（分支名称）"),
          ("switchCommandParaExtendName", "string", "开关参数扩展名称"),
          ("switchCommandParaName", "string", "开关命令参数名称"),
          ("switchEnumItemId", "integer", "开关枚举项ID（分支枚举）"),
          ("enumItemName", "string", "枚举项名称"),
          ("commandBranchTableId", {"type": "array", "items": {"type": "integer"},
                                    "description": "命令分支表ID列表"}, "命令分支表ID列表"),
          ("childCommandParaDtos", arr_of(child_para, "子命令参数列表"), "子命令参数列表")],
         ["commandId", "taskId", "switchCommandParaId", "switchEnumItemId"]),
    ["CommandBranch"])

# 24. 查询命令分支列表
add("/api/commandBranch/list", "post", "查询命令分支列表",
    body([("commandId", "integer", "命令ID")], ["commandId"]),
    ["CommandBranch"], data=DATA_ARR)

# 25. 查询所有枚举项
add("/list/customize/datatype/enum/queryAll", "post", "根据数据类型名称ID查询所有枚举项",
    body([("enumDatatypeNameId", "integer", "枚举数据类型名称ID")],
         ["enumDatatypeNameId"]),
    ["Datatype"], data=DATA_ARR)

# 26. 模型数据校验
add("/api/validate/do", "post", "执行模型数据校验",
    body([("projectId", "integer", "项目/任务ID（必须为Integer类型）"),
          ("isCommitAndPush", "string", '是否提交并推送（"1"=git提交+推送，仅屏蔽3001；"0"/空=普通校验）')],
         ["projectId"]),
    ["Validate"], data=DATA_OBJ)

# 27. 获取校验结果
add("/api/validate/result", "post", "从数据库获取存储的校验结果",
    body([("projectId", "integer", "项目/任务ID（必须为Integer类型）"),
          ("level", "string", "校验结果级别过滤（ERROR/WARNING/INFO）"),
          ("searchName", "string", "搜索名称过滤")],
         ["projectId"]),
    ["Validate"], data=DATA_ARR)

# 28. 屏蔽错误码
add("/api/errorcode/shield", "post", "设置需要屏蔽的错误码规则",
    body([("id", "integer", "主键（修改时传入）"),
          ("projectId", "integer", "项目/任务ID"),
          ("shieldErrorCodes", "string", "屏蔽的错误码，逗号分隔")],
         ["projectId", "shieldErrorCodes"]),
    ["ErrorCode"])

# 29. 导出Go Struct信息（第三个路径段实际为 isIncludeAlarmCarding）
add("/sbbapi/task/exportStruct/{taskId}/{taskName}/{version}", "post",
    "导出Go Struct信息（异步任务）",
    [path_param("taskId", "string", "任务ID"),
     path_param("taskName", "string", "任务名称"),
     path_param("version", "integer", "实际为isIncludeAlarmCarding：是否包含告警梳理（0/1）")],
    ["Task"])

# 30. 获取导出Go Struct结果
add("/sbbapi/task/getExportStructResult/{taskId}/{taskName}", "get",
    "获取导出Go Struct的异步任务结果",
    [path_param("taskId", "string", "任务ID"),
     path_param("taskName", "string", "任务名称（后端未使用）")],
    ["Task"], data=DATA_OBJ)

# 31. 下载导出文件 (binary)
add("/sbbapi/task/download/{taskId}/{taskName}", "get",
    "下载导出的Go Struct信息结果文件",
    [path_param("taskId", "string", "任务ID"),
     path_param("taskName", "string", "任务名称")],
    ["Task"], produces=["application/octet-stream"])

# 32. 添加错误码
info_code = obj([
    ("infoModuleId", "integer", "信息模块ID"),
    ("infoCodeName", "string", "错误码名称"),
    ("infoCodeNum", "integer", "错误码编号"),
    ("infoCodeChDesc", "string", "错误码中文描述"),
    ("infoCodeEnDesc", "string", "错误码英文描述"),
    ("infoCodeSource", "string", "错误码来源"),
    ("isCustom", "integer", "是否自定义"),
], required=["infoModuleId", "infoCodeName"], desc="错误码对象")
add("/list/infoCode/add", "post", "添加错误码",
    body([("projectId", "integer", "项目/任务ID"),
          ("infoCode", info_code, "错误码对象")],
         ["projectId", "infoCode"]),
    ["InfoCode"])

# 33. 查询错误码列表 (后端实际 /infoCode/queryAll)
add("/list/infoCode/list", "post", "查询错误码列表（后端实际路径 /infoCode/queryAll）",
    body([("projectId", "integer", "项目/任务ID"),
          ("infoModuleId", "integer", "信息模块ID（过滤）")],
         ["projectId"]),
    ["InfoCode"], data=DATA_ARR)

# 34. 查询所有信息模块
add("/list/infoModule/queryAll", "post", "根据项目ID查询所有信息模块",
    body([("projectId", "integer", "项目/任务ID")], ["projectId"]),
    ["InfoModule"], data=DATA_ARR)

# 35. 插入MOC信息
add("/sbbapi/moc/insertMocInfo", "post", "插入/更新MOC完整信息",
    body([("mocId", "string", "MOC ID（数字）"),
          ("taskId", "string", "任务ID（数字）"),
          ("mocName", "string", "MOC名称"),
          ("mocDescCh", "string", "MOC中文描述"),
          ("mocDescEn", "string", "MOC英文描述"),
          ("mocTypeId", "string", "MOC类型ID"),
          ("moduleId", "string", "模块ID"),
          ("m2k", "string", "网管可见性"),
          ("minRecordNum", "string", "最小记录数"),
          ("maxRecordNum", "string", "最大记录数"),
          ("maxRecords", "string", "最大记录"),
          ("blankMode", "string", "空白模式"),
          ("batchDelete", "string", "批量删除"),
          ("version", "string", "版本（配置类必填）"),
          ("isProcessReport", "string", "是否流程报告"),
          ("sceneReference", "string", "场景引用"),
          ("recUpgMode", "string", "记录升级模式"),
          ("recCopyMode", "string", "记录拷贝模式"),
          ("passwordExportMode", "string", "密码导出模式"),
          ("almThreshold", "string", "告警阈值"),
          ("almEcoveryThreshold", "string", "告警恢复阈值"),
          ("publicMode", "string", "公共模式"),
          ("associateInfo", "string", "关联组件信息"),
          ("associatedComponentId", "string", "关联组件DB ID"),
          ("subscribeModuleInfo", "string", "订阅模块信息"),
          ("mocTypeName", "string", "MOC类型名称（UDG映射使用）"),
          ("oldMocName", "string", "旧MOC名称（重命名检测）"),
          ("objectId", "string", "对象ID")],
         ["mocId", "taskId", "mocName", "mocDescCh", "mocDescEn"]),
    ["Moc"])

# 36. 生成LUA脚本文件 (binary)
add("/sbbapi/moc/generateScriptFile/{taskId}/{mocId}/{mocName}/{scriptOper}/{isGenerateCode}",
    "get", "生成LUA脚本文件并下载",
    [path_param("taskId", "string", "任务ID"),
     path_param("mocId", "integer", "MOC ID"),
     path_param("mocName", "string", "MOC名称"),
     path_param("scriptOper", "string", "脚本操作类型"),
     path_param("isGenerateCode", "integer", "是否生成代码（0/1）")],
    ["Moc"], produces=["application/octet-stream"])


# 37. 搜索项目模块概览
add("/myapi/overallview/search", "post", "搜索项目模块概览",
    body([("projectId", "string", "项目/任务ID")], ["projectId"]),
    ["OverallView"],
    data={"type": "array", "description": "模块概览列表",
          "items": {"type": "object", "properties": {
              "id": {"type": "integer", "description": "模块ID"},
              "serviceName": {"type": "string", "description": "服务名称"},
              "moduleName": {"type": "string", "description": "模块名称"},
          }}})

# 38. 删除工程/任务（破坏性）
add("/api/task/deleteOne", "post", "删除单个工程/任务",
    body([("taskId", "integer", "要删除的工程/任务ID（数字）")], ["taskId"]),
    ["Task"],
    description='破坏性操作：删除后不可恢复。响应为 {"status":true} 形式，status=false 表示删除失败。',
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "是否删除成功"},
    }}))

# ---------------------------------------------------------------------------
# 性能指标注册（指标组/测量单元 + 指标）
#
# 这几个接口有严格的先后顺序，前一步拿到的 ID 是后一步的入参：
#   getAllMicroService  → belongService
#   idRange/autoGenId      idType=mu     → muId
#   northIdRange/autoGenId idType=mu     → nmMuId
#   indicatorGroup/insert                → 建指标组
#   idRange/autoGenId      idType=metric → metricId（要带 muId）
#   indicator/manage                     → 登记指标ID与名称
#   northIdRange/autoGenId idType=metric → nmMetricId
#   indicator                            → 补齐指标完整属性
# 它们统一以 {"status":false} 表达业务失败，不是 {code:0}。

AUTOGEN_RESP = raw_resp({"type": "object", "properties": {
    "status": {"type": "boolean", "description": "操作是否成功"},
    "data": {"type": "integer", "description": "生成的ID值"},
    "message": {"type": "string", "description": "提示信息，成功时为null"},
}})

PERF_RESP = status_resp({"type": "string", "description": "返回数据，通常为空"},
                        "提示信息，如「新增成功。」")

# 39. 自动生成本地测量单元ID/指标ID
add("/api/resource/perf/idRange/autoGenId", "get", "自动生成本地测量对象/测量单元/指标ID",
    [query_param("neName", "string", True, "NE名称，如 UNC"),
     query_param("belongService", "integer", True, "归属服务ID，如 203"),
     query_param("idType", "string", True, "ID类型：mu=测量单元ID，metric=指标ID，moc=测量对象ID"),
     query_param("taskId", "integer", True, "任务ID，如 47754"),
     query_param("muId", "integer", False, "MU ID，如 9（idType=mu/moc 时可留空）")],
    ["Resource"],
    description="按 idType 生成本地ID：moc=测量对象ID（mocId）、mu=测量单元ID（muId）、metric=指标ID（metricId）。"
                "idType=metric 时必须带 --muId 指明指标挂在哪个测量单元下；moc/mu 可以不带。"
                "本接口与 northIdRange/autoGenId 是两套ID空间，belongService 也不同，不要混用。",
    responses=AUTOGEN_RESP)

# 40. 查询全部微服务
add("/myapi/overallview/getAllMicroService", "get", "查询全部微服务", [], ["OverallView"],
    description="返回全部微服务信息，用于把「实际服务列表」（如 SmcExecSvc）映射到注册指标组/指标时要用的 belongService 服务ID。无入参。",
    responses=raw_resp({
        "type": "object",
        "description": "微服务列表（含服务ID与服务名）。后端返回结构原样透传，不做裁剪。",
    }))

# 41. 自动生成网管侧（北向）测量单元ID/指标ID
add("/api/resource/perf/northIdRange/autoGenId", "get",
    "自动生成网管侧（北向）测量对象/测量单元/指标ID",
    [query_param("neName", "string", True, "NE名称，如 UNC"),
     query_param("belongService", "integer", True, "网管侧归属服务ID，如 114"),
     query_param("idType", "string", True, "ID类型：moc=网管测量对象ID，mu=网管测量单元ID，metric=网管指标ID"),
     query_param("checkDeleted", "boolean", False, "是否复用已删除的ID，默认 false")],
    ["Resource"],
    description="生成向网管注册时使用的 nmMocId（idType=moc）、nmMuId（idType=mu）或 nmMetricId（idType=metric）。"
                "注意 belongService 用的是网管侧服务ID（如 114），与 idRange/autoGenId 的服务ID（如 203）不是同一个。",
    responses=AUTOGEN_RESP)

# 42. 新建指标组（测量单元）
add("/api/perf/object/indicatorGroup/insert", "post", "新建指标组（测量单元）并向网管注册",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754")] +
    body([("mocId", "integer", "对象（MOC）ID，如接入类型对应的 MOC ID"),
          ("mocChName", "string", "对象中文名，如 RATTYPE"),
          ("belongService", "integer", "归属服务ID，如 203（由 overallview micro-service-list 查得）"),
          ("serviceInfo", "string", "托管微服务，如 basicBizService/ompublic"),
          ("realServiceNames", {"type": "array",
                                "description": '实际服务列表，如 ["SmcExecSvc"]',
                                "items": {"type": "string"}}, "实际服务列表"),
          ("realServicesName", "string", "实际服务列表的字符串形式，多个用逗号分隔，如 SmcExecSvc"),
          ("nmMfId", "integer", "所属功能集ID，如 1929445378（SMF会话管理）"),
          ("muId", "string", "测量单元ID，由 resource auto-gen-id --idType mu 获取"),
          ("muName", "string", "测量单元名称，如「指定RATTYPE的CGW 4G会话管理失败流程」"),
          ("muChMeaning", "string", "测量单元含义（中文）"),
          ("muEnMeaning", "string", "测量单元含义（英文）"),
          ("monitorType", "integer", "监控类型，1=性能统计"),
          ("dimensionsCalc", "string", "是否向父对象聚合指标：是/否"),
          ("isRealTimeMonitor", "string", "是否支持实时监控：是/否"),
          ("defaultPeriod", "string", "默认任务周期（分钟），如 5"),
          ("defaultReportBasicMe", "string", "默认上报的基础指标，可为空串"),
          ("isCombine", "string", "是否支持复合周期：是/否"),
          ("defaultMetricRange", "string", "默认指标范围，可为 null"),
          ("isHide", "string", "是否隐藏：是/否"),
          ("stringResId", "string", "语言资源ID，格式 MU_<测量单元ID>，如 MU_12156"),
          ("monitorId", "string", "监控ID，可为 null"),
          ("perfIndsMacroDefine", "string", "性能指标宏定义，可为空串"),
          ("nmMuId", "string", "网管测量单元ID，由 resource north-auto-gen-id --idType mu 获取")],
         ["belongService", "muId", "muName", "monitorType", "stringResId", "nmMuId"]),
    ["Perf"],
    description="新增性能测量单元（指标组）。muId 来自 resource auto-gen-id --idType mu，"
                "nmMuId 来自 resource north-auto-gen-id --idType mu，二者必须先取到再调用本接口。"
                "响应 status=false 表示业务失败。",
    responses=PERF_RESP)

# 43. 在指标组下登记指标ID与名称
add("/api/perf/object/indicator/manage", "post", "在指标组下新建指标（登记指标ID与名称）",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754"),
     query_param("belongService", "integer", True, "归属服务ID，如 203")] +
    body([("metricId", "string", "指标ID，由 resource auto-gen-id --idType metric --muId <muId> 获取"),
          ("metricName", "string", "指标名称"),
          ("meType", "integer", "指标类型，0=数值指标"),
          ("belongService", "integer", "归属服务ID，如 203"),
          ("muId", "integer", "所属指标组（测量单元）ID")],
         ["metricId", "metricName", "meType", "belongService", "muId"]),
    ["Perf"],
    description="把 resource auto-gen-id --idType metric 拿到的指标ID登记到指定测量单元下。"
                "登记后再调用 perf indicator-update 补齐指标的完整属性。响应 status=false 表示业务失败。",
    responses=PERF_RESP)

# 44. 保存指标完整属性（向网管注册指标）
add("/api/perf/object/indicator", "post", "保存指标完整属性（向网管注册指标）",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754"),
     query_param("metricId", "integer", True, "指标ID，需与请求体的 metricId 一致"),
     query_param("belongService", "integer", True, "归属服务ID，如 203")] +
    body([("muId", "integer", "所属指标组（测量单元）ID"),
          ("metricId", "integer", "指标ID"),
          ("metricName", "string", "指标名称"),
          ("meType", "integer", "指标类型，0=数值指标"),
          ("belongService", "integer", "归属服务ID，如 203"),
          ("zoom", "string", "指标值修正系数，如 1"),
          ("isNeedMultiCalc", "string", "是否支持单指标多算法：是/否"),
          ("serviceCalcMode", "string", "服务实例间计算方式，如 ACCL"),
          ("dimensionCalcMode", "string", "维度间计算方式，如 ACCL"),
          ("periodCalcMode", "string", "复合周期间计算方式，如 ACCL"),
          ("formular", "string", "计算公式，普通数值指标留空"),
          ("valueType", "string", "指标值类型，如 INT32"),
          ("metricUnitName", "string", "指标单位名称，如「个」"),
          ("meStringResId", "string", "指标语言资源ID，格式 <组件名>_<指标ID>，如 SMC_13718"),
          ("meUnitStringResId", "string", "指标单位资源ID，如 UNIT_0"),
          ("defaultValue", "string", "指标初始默认值，如 0"),
          ("meMinValue", "string", "指标最小值，可为 null"),
          ("meMaxValue", "string", "指标最大值，可为 null"),
          ("isServiceCalcExcludeDefaultValue", "string", "是否参与实例间计算：是/否"),
          ("isOpen2ui", "string", "是否开放本地UI：是/否"),
          ("isOpen2nm", "string", "是否开放网管：是/否"),
          ("nmMetricId", "string", "网管指标ID，由 resource north-auto-gen-id --idType metric 获取"),
          ("isHide", "string", "是否隐藏指标：是/否"),
          ("isKpiCheck", "string", "是否支持指标检测：是/否"),
          ("perfindZhMeaningname", "string", "性能指标含义（中文）"),
          ("perfindEnMeaningname", "string", "性能指标含义（英文）"),
          ("measuringpoinZhDesc", "string", "测量点描述（中文）"),
          ("measuringpoinEnDesc", "string", "测量点描述（英文）"),
          ("measurementType", "string", "测量类型，如「当统计周期时间大于采集周期时，取统计周期内采集周期的累加值」"),
          ("isHaveGraphic", "string", "是否有图示：是/否"),
          ("graphicId", "string", "图ID，如 fig<网管指标ID>01.png"),
          ("graphicCh", "string", "图示（中文）"),
          ("graphicEn", "string", "图示（英文）"),
          ("relatedZhNote", "string", "图注（中文）"),
          ("relatedEnNote", "string", "图注（英文）"),
          ("isProductKpi", "string", "是否产品KPI，可为 null"),
          ("label", "string", "标签，可为空串"),
          ("orderValue", "integer", "指标排序值"),
          ("realServicesName", "string", "实际服务列表，可为空串"),
          ("instanceProp", "string", "实例属性，可为空串"),
          ("podPeriod", "string", "pod周期，可为空串"),
          ("isBasicMe", "string", "是否基础指标，可为 null"),
          ("monitorId", "string", "监控ID，可为 null"),
          ("isSupport5sMonitor", "string", "是否支持5秒监控，可为 null"),
          ("isDefaultReport", "string", "是否默认上报，可为 null"),
          ("isMoreThanBillion", "string", "指标值是否可能超过十亿，可为 null"),
          ("isScanMe", "string", "是否扫描指标，可为 null"),
          ("scanPeriod", "string", "扫描周期，可为 null"),
          ("scanType", "string", "扫描类型，可为 null"),
          ("assistMeId", "string", "辅助指标ID，可为 null"),
          ("convergenceNode", "string", "汇聚节点，可为 null"),
          ("scanMultiInstanceCalcMode", "string", "扫描指标多实例计算方式，可为 null"),
          ("scanMultiPeriodCalcMode", "string", "扫描指标多周期计算方式，可为 null"),
          ("precisionConvertMultiple", "string", "精度转换倍数，可为 null"),
          ("meUseDomain", "string", "指标使用域，可为 null"),
          ("switchDotAreaConfig", "string", "开关点区域配置，可为 null"),
          ("kpiTypeId", "string", "KPI类型ID，可为 null"),
          ("kpiZhType", "string", "KPI类型（中文）"),
          ("kpiEnType", "string", "KPI类型（英文）"),
          ("kpiReferenceRangeZh", "string", "KPI参考范围（中文）"),
          ("kpiReferenceRangeEn", "string", "KPI参考范围（英文）"),
          ("isSetDefaultCfg", "string", "是否设置默认配置"),
          ("detectionPeriod", "string", "检测周期"),
          ("checkAlgorithm", "string", "检测算法"),
          ("upperThreshold", "string", "上门限"),
          ("lowerThreshold", "string", "下门限"),
          ("chainUpThld", "string", "环比上门限"),
          ("chainLowThld", "string", "环比下门限"),
          ("minDetectValue", "string", "最小检测值"),
          ("isReportAlarms", "string", "是否上报告警"),
          ("moiName", "string", "MOI名称"),
          ("perfIndthresholdceiling", "string", "性能指标门限上限，可为 null"),
          ("perfIndThresholdBottom", "string", "性能指标门限下限，可为 null"),
          ("performanceAndMonitor", "string", "性能与监控，可为 null"),
          ("netconf", "string", "netconf 配置，可为 null"),
          ("isDefineMetric", "string", "是否自定义指标，可为 null"),
          ("isSupportUsc", "string", "是否支持USC，可为 null"),
          ("perfCounter", "string", "性能计数器，可为 null"),
          ("perfType", "string", "性能类型，可为 null"),
          ("openToUI", "string", "是否开放UI（旧字段），可为 null"),
          ("isCacKeyMetric", "string", "是否CAC关键指标，可为 null"),
          ("vformular", "string", "虚拟指标公式"),
          ("isResourceMe", "string", "是否资源类指标，可为 null"),
          ("fluctuationCfg", "string", "波动配置，可为 null"),
          ("sequentialVolatility", "string", "环比波动率，可为 null")],
         ["muId", "metricId", "metricName", "meType", "belongService",
          "valueType", "meStringResId", "nmMetricId"]),
    ["Perf"],
    description="在 perf indicator-add 登记指标ID之后调用，写入算法、值类型、语言资源、测量点、网管指标ID等完整属性。"
                "查询参数中的 metricId 必须与请求体中的 metricId 一致。"
                "请求体字段较多，未在此列出的后端字段会原样透传，不做裁剪。响应 status=false 表示业务失败。",
    responses=PERF_RESP)


# ---------------------------------------------------------------------------
# 告警建模（45~55）
#
# 这批接口返回的是 {status, message} / {status, message, data}，不是 {code,msg,data}，
# 所以统一用 ALARM_OK / alarm_list_resp，不要走默认的 resp()。
ALARM_OK = raw_resp({
    "type": "object",
    "properties": {
        "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
        "message": {"type": "string", "description": "提示信息"},
    },
})


def alarm_list_resp(item_obj, desc):
    return status_resp(arr_of(item_obj, desc), "提示信息")


# 告警内部主键 id 与后端分配的告警号 alarmId 是两个东西，反复出现，抽出来统一措辞。
ALARM_INTERNAL_ID = "所属告警的内部主键ID（alarm list 返回的 id，不是告警号 alarmId）"
ALARM_ID_IS_INTERNAL = "告警内部主键ID（alarm list 返回的 id，不是告警号 alarmId）"


def service_fields(id_type, id_desc):
    return [("id", id_type, id_desc),
            ("serviceName", "string", "告警服务名称"),
            ("microServiceType", "string", "微服务类型，如 udgService / basicBizService"),
            ("microServiceName", "string", "微服务名称，如 udgompublic / ompublic")]


def alarm_fields(id_type, id_desc):
    return [
        ("id", id_type, id_desc),
        ("serviceId", "integer", "所属告警服务ID，取自 alarm-service list 返回的 id"),
        ("alarmId", "string", "告警号，由后端分配（如 \"100910\"），注意与内部主键 id 不是一回事"),
        ("mocName", "string", "MOC名称"),
        ("alarmEnglishName", "string", "告警英文名"),
        ("alarmChineseName", "string", "告警中文名"),
        ("alarmTypeId", "integer", "告警类型ID"),
        ("alarmLevelId", "integer", "告警级别ID"),
        ("eventTypeId", "integer", "事件类型ID"),
        ("isCheck", "integer", "是否核查"),
        ("isGlobal", "integer", "是否全局告警"),
        ("isAutoClean", "integer", "是否自动清除"),
        ("transientPeriod", "integer", "瞬断周期（秒），默认90"),
        ("toggleStartPeriod", "integer", "频繁告警统计起始周期（秒），默认300"),
        ("toggleEndPeriod", "integer", "频繁告警统计结束周期（秒），默认300"),
        ("toggleThreshold", "integer", "频繁告警阈值，默认3"),
        ("alarmName", "string", "告警名称标识"),
        ("northObjType", "string", "北向对象类型"),
        ("alarmCreReasonCh", "string", "告警产生原因（中文）"),
        ("alarmCreReasonEn", "string", "告警产生原因（英文）"),
        ("isSuppress", "integer", "是否抑制"),
        ("largeParticlesType", "integer", "大颗粒类型"),
        ("nlsType", "string", "NLS类型"),
        ("assist", "string", "辅助信息"),
        ("ossAlarmLevel", "string", "OSS告警级别，如 \"三级告警\""),
        ("alarmExplain", "string", "告警解释"),
        ("applyNe", "string", "适用网元"),
        ("mobileLogicClassify", "string", "移动-逻辑分类，如 \"硬件告警\""),
        ("mobileLogicClassifyChild", "string", "移动-逻辑子分类，如 \"CPU硬件告警\""),
        ("mobileAffectToDevice", "string", "移动-对设备的影响"),
        ("mobileAffectToBusiness", "string", "移动-对业务的影响"),
        ("mobileIsRelatedPeer", "string", "移动-是否关联对端，\"是\"/\"否\""),
        ("unicomLogicClassify", "string", "联通-逻辑分类"),
        ("unicomLogicClassifyChild", "string", "联通-逻辑子分类"),
        ("unicomAffectToDevice", "string", "联通-对设备的影响"),
        ("unicomAffectToBusiness", "string", "联通-对业务的影响"),
    ]


def enum_fields(id_type, id_desc, with_list=False):
    f = [("id", id_type, id_desc),
         ("alarmInternalId", "integer", ALARM_INTERNAL_ID),
         ("enumType", "string", "枚举类型"),
         ("enumName", "string", "枚举类型名称")]
    if with_list:
        f.append(("alarmEnumList",
                  {"type": "array", "items": {"type": "object"}},
                  "该枚举类型下的枚举值列表，list 接口通常返回 null"))
    return f


def para_fields(id_type, id_desc, length_type):
    return [
        ("id", id_type, id_desc),
        ("alarmInternalId", "integer", ALARM_INTERNAL_ID),
        ("paramEnglishAbbreviationName", "string", "参数英文缩写名，新增时只需传这一个字段"),
        ("paramEnglishName", "string", "参数英文名"),
        ("paramChineseName", "string", "参数中文名"),
        ("paramLength", length_type, "参数长度"),
        ("paramClassId", "integer", "参数类别ID"),
        ("paramTypeId", "integer", "参数类型ID"),
        ("enumTypeId", "integer", "关联的枚举类型ID，取自 alarm-enum list 返回的 id"),
        ("alarmId", "integer", "关联告警ID"),
        ("paramMeanCh", "string", "参数含义（中文）"),
        ("paramMeanEn", "string", "参数含义（英文）"),
        ("isAllowEdit", "integer", "是否允许编辑"),
        ("paramOrder", "integer", "参数顺序，取自 alarm-para list 返回的 paramOrder"),
    ]


# 45. 新增或修改告警服务
add("/api/alarmService/insertOrUpdate", "post", "新增或修改告警服务",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmServiceTable",
           obj(service_fields("string", "告警服务内部主键ID。新增传空字符串 \"\"，修改传 alarm-service list 返回的 id"),
               ["serviceName"], "告警服务信息"),
           "告警服务信息")],
         ["taskId", "alarmServiceTable"]),
    ["Alarm"], responses=ALARM_OK)

# 46. 查询告警服务列表
add("/api/alarmService/list", "post", "查询告警服务列表",
    body([("taskId", "integer", "工程/任务ID")], ["taskId"]),
    ["Alarm"],
    description="新建告警服务后用本接口按 serviceName 反查其 id，该 id 即后续告警接口的 serviceId。",
    responses=alarm_list_resp(
        obj(service_fields("integer", "告警服务内部主键ID，即后续的 serviceId"), None, "告警服务"),
        "告警服务列表"))

# 47. 新增或修改告警（含告警配置信息）
add("/api/alarm/insertOrUpdate", "post", "新增或修改告警（含告警配置信息）",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmTable",
           obj(alarm_fields("string", "告警内部主键ID。新增传空字符串 \"\"，修改传 alarm list 返回的 id"),
               ["serviceId", "alarmChineseName", "alarmEnglishName"],
               "告警信息，新建时未知字段可省略或传 null"),
           "告警信息")],
         ["taskId", "alarmTable"]),
    ["Alarm"],
    description="同一接口覆盖两种用法：新建时只传 serviceId + 中英文名（id 传空字符串），"
                "后端分配 alarmId；补全配置时传回 list 拿到的 id、serviceId、alarmId 再带上级别/分类等字段。",
    responses=ALARM_OK)

# 48. 查询指定告警服务下的告警列表
add("/api/alarm/list", "post", "查询指定告警服务下的告警列表",
    body([("taskId", "integer", "工程/任务ID"),
          ("serviceId", "integer", "告警服务ID，取自 alarm-service list 返回的 id")],
         ["taskId", "serviceId"]),
    ["Alarm"],
    description="响应里的 id 是告警内部主键（后续 alarmInternalId 用它），"
                "alarmId 是后端分配的告警号字符串，两者不要混用。",
    responses=alarm_list_resp(
        obj(alarm_fields("integer", "告警内部主键ID，即后续的 alarmInternalId"), None, "告警"),
        "告警列表"))

# 49. 新增或修改告警枚举类型
add("/api/alarmEnum/insertOrUpdate", "post", "新增或修改告警枚举类型",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmEnumTable",
           obj(enum_fields("string", "枚举类型内部主键ID。新增传空字符串 \"\"，修改传 alarm-enum list 返回的 id"),
               ["alarmInternalId", "enumType", "enumName"], "告警枚举类型信息"),
           "告警枚举类型信息")],
         ["taskId", "alarmEnumTable"]),
    ["Alarm"], responses=ALARM_OK)

# 50. 查询告警枚举类型列表
add("/api/alarmEnum/list", "post", "查询告警枚举类型列表",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmId", "integer", ALARM_ID_IS_INTERNAL)],
         ["taskId", "alarmId"]),
    ["Alarm"],
    description="注意入参名虽叫 alarmId，实际要传告警的内部主键 id（alarm list 返回的 id），不是告警号字符串。",
    responses=alarm_list_resp(
        obj(enum_fields("integer", "枚举类型ID，即创建枚举值时的 enumTypeId", with_list=True), None, "枚举类型"),
        "枚举类型列表"))

# 51. 新增或修改告警枚举值
add("/api/alarmEnumValue/insertOrUpdate", "post", "新增或修改告警枚举值",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmEnumValueTable",
           obj([("id", "string", "枚举值内部主键ID。新增传空字符串 \"\" 或省略"),
                ("enumTypeId", "integer", "所属枚举类型ID，取自 alarm-enum list 返回的 id"),
                ("iValue", "string", "枚举整型值"),
                ("englishSvalue", "string", "枚举英文名"),
                ("chineseSvalue", "string", "枚举中文名"),
                ("itemMeanEn", "string", "枚举含义（英文），无内容填 NA"),
                ("itemMeanCh", "string", "枚举含义（中文）")],
               ["enumTypeId", "iValue"], "告警枚举值信息"),
           "告警枚举值信息")],
         ["taskId", "alarmEnumValueTable"]),
    ["Alarm"], responses=ALARM_OK)

# 52. 新增或修改告警参数
add("/api/alarmPara/insertOrUpdate", "post", "新增或修改告警参数",
    body([("taskId", "integer", "工程/任务ID"),
          ("isAllowEdit", "integer", "是否允许编辑，与 alarmParaTable 平级，新增参数时传 1"),
          ("alarmParaTable",
           obj(para_fields("string",
                           "告警参数内部主键ID。新增传空字符串 \"\" 或省略，补全时传 alarm-para list 返回的 id",
                           "string"),
               ["alarmInternalId", "paramEnglishAbbreviationName"], "告警参数信息"),
           "告警参数信息")],
         ["taskId", "alarmParaTable"]),
    ["Alarm"],
    description="分两步用：先只传 alarmInternalId + paramEnglishAbbreviationName 建参数占位"
                "（可带 isAllowEdit=1），再用 alarm-para list 拿到 id 与 paramOrder 后回传完整字段补全。",
    responses=ALARM_OK)

# 53. 查询告警参数列表
add("/api/alarmPara/list", "post", "查询告警参数列表",
    body([("taskId", "integer", "工程/任务ID"),
          ("alarmId", "integer", ALARM_ID_IS_INTERNAL)],
         ["taskId", "alarmId"]),
    ["Alarm"],
    description="注意入参名虽叫 alarmId，实际要传告警的内部主键 id（alarm list 返回的 id），不是告警号字符串。",
    responses=alarm_list_resp(
        obj(para_fields("integer", "告警参数内部主键ID", "integer"), None, "告警参数"),
        "告警参数列表"))

# 54. 导出前校验
add("/sbbapi/task/exportValidate/", "post", "导出前校验（路径末尾的斜杠是后端要求，不能去掉）",
    body([("taskId", "integer", "工程/任务ID")], ["taskId"]),
    ["Task"],
    responses=raw_resp({
        "type": "object",
        "properties": {
            "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
            "show": {"type": "boolean", "description": "校验结果是否需要展示"},
            "message": {"type": "string", "description": "提示信息"},
        },
    }))

# 55. 判断工程是否含有告警
add("/api/task/isTaskIncludeAlarm", "post", "判断工程是否含有告警（multipart 表单，不是 JSON）",
    [form_param("taskId", "string", True, "工程/任务ID")],
    ["Task"], consumes=["multipart/form-data"],
    responses=raw_resp({
        "type": "object",
        "properties": {
            "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
            "message": {"type": "string", "description": "提示信息，如 \"工程含有告警\""},
        },
    }))

# ---------------------------------------------------------------------------
# 语言资源与删除类接口（56~60）
#
# 语言资源用来给测量单元/指标提供中英文描述：指标组的 stringResId（MU_<muId>）、
# 指标的 meStringResId（<组件名>_<指标ID>）都要有对应的语言资源记录。
# 删除类接口的请求体是「记录数组」，通常直接把查询接口返回的整条记录回传即可。

LANG_RES_FIELDS = [
    ("stringResId", "string", "语言资源ID：测量单元用 MU_<muId>（如 MU_55），指标用 <组件名>_<指标ID>（如 SMC_10000）"),
    ("descriptionZh", "string", "中文描述"),
    ("descriptionEn", "string", "英文描述"),
    ("remark", "string", "备注，可为 null"),
    ("belongService", "integer", "归属服务ID，如 203"),
]

# 56. 注册语言资源
add("/api/perf/baseInfo/languageResource/insert", "post", "注册语言资源（测量单元/指标的中英文描述）",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754")] +
    body([(n, t, d) for n, t, d in LANG_RES_FIELDS if n != "remark"],
         ["stringResId", "descriptionZh", "descriptionEn", "belongService"]),
    ["Perf"],
    description="为指标组或指标登记语言资源。stringResId 要与指标组的 stringResId / 指标的 meStringResId 一致，"
                "否则前端展示不出中英文描述。请求体里的 belongService 可以是字符串（如 \"203\"），后端按数字处理。"
                "响应 status=false 表示业务失败。",
    responses=PERF_RESP)

# 57. 批量删除语言资源（破坏性）
add("/api/perf/baseInfo/languageResource/delete/batch", "post", "批量删除语言资源",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754")] +
    arr_body(obj(LANG_RES_FIELDS, ["stringResId"], "要删除的语言资源记录"),
             "要删除的语言资源列表，通常把注册时的记录原样回传"),
    ["Perf"],
    description="破坏性操作：请求体是数组，每个元素是一条完整的语言资源记录。"
                "data 返回实际删除的条数。响应 status=false 表示业务失败。",
    responses=status_resp({"type": "integer", "description": "实际删除的记录条数"},
                          "提示信息，成功时通常为 null"))

# 58. 批量删除指标组（测量单元）（破坏性）
DELETE_GLOBAL_RESP_PROPS = {
    "monitorGlobal": {"type": "array", "items": {"type": "object"},
                      "description": "受影响的监控全局配置，为空表示无残留引用"},
    "nmGlobal": {"type": "array", "items": {"type": "object"},
                 "description": "受影响的网管侧全局配置"},
    "innerGlobal": {"type": "array", "items": {"type": "object"},
                    "description": "受影响的内部全局配置"},
}

add("/api/perf/object/indicatorGroup/delete/batch", "post", "批量删除指标组（测量单元）",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754")] +
    arr_body(obj([("muId", "integer", "测量单元ID"),
                  ("muName", "string", "测量单元名称"),
                  ("mocId", "integer", "对象（MOC）ID"),
                  ("belongService", "integer", "归属服务ID，如 203"),
                  ("realServicesName", "string", "实际服务列表，如 SmcExecSvc"),
                  ("monitorType", "integer", "监控类型，1=性能统计"),
                  ("stringResId", "string", "语言资源ID，格式 MU_<测量单元ID>"),
                  ("nmMuId", "integer", "网管测量单元ID"),
                  ("nmMfId", "integer", "所属功能集ID"),
                  ("isHide", "string", "是否隐藏：是/否")],
                 ["muId", "belongService"],
                 "要删除的指标组记录，字段与 perf indicator-group-add 的请求体一致，"
                 "未列出的字段原样透传"),
             "要删除的指标组列表"),
    ["Perf"],
    description="破坏性操作：请求体是数组，元素是完整的指标组记录（可直接回传创建/查询时的记录）。"
                "删除指标组前应先删掉其下的指标。响应的 data.length 是删除条数，"
                "其余数组列出被牵连的全局配置。响应 status=false 表示业务失败。",
    responses=status_resp(
        obj([("length", "integer", "实际删除的指标组条数"),
             ("monitorGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的监控全局配置，为空表示无残留引用"}, ""),
             ("monitorMetricGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的监控指标全局配置"}, ""),
             ("nmGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的网管侧全局配置"}, ""),
             ("nmMetricGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的网管侧指标全局配置"}, ""),
             ("innerGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的内部全局配置"}, ""),
             ("innerMetricGlobal", {"type": "array", "items": {"type": "object"}, "description": "受影响的内部指标全局配置"}, "")],
            None, "删除结果"),
        "提示信息，成功时通常为空串"))

# 59. 批量删除指标（破坏性）
add("/api/perf/object/indicator/manage/delete", "post", "批量删除指标",
    [query_param("taskId", "integer", True, "任务/工程ID，如 47754")] +
    arr_body(obj([("metricId", "integer", "指标ID"),
                  ("metricName", "string", "指标名称"),
                  ("muId", "integer", "所属指标组（测量单元）ID"),
                  ("meType", "integer", "指标类型，0=数值指标"),
                  ("belongService", "integer", "归属服务ID，如 203"),
                  ("valueType", "string", "指标值类型，如 INT32"),
                  ("meStringResId", "string", "指标语言资源ID，如 SMC_10000"),
                  ("meUnitStringResId", "string", "指标单位资源ID，如 UNIT_0"),
                  ("nmMetricId", "integer", "网管指标ID"),
                  ("isHide", "string", "是否隐藏：是/否")],
                 ["metricId", "muId", "belongService"],
                 "要删除的指标记录，字段与 perf indicator-update 的请求体一致，"
                 "未列出的字段原样透传"),
             "要删除的指标列表"),
    ["Perf"],
    description="破坏性操作：请求体是数组，元素是完整的指标记录（可直接回传 perf indicator-update 用过的记录）。"
                "响应的三个数组列出被牵连的全局配置，为空表示没有残留引用。响应 status=false 表示业务失败。",
    responses=status_resp(
        {"type": "object", "description": "删除结果", "properties": DELETE_GLOBAL_RESP_PROPS},
        "提示信息，成功时通常为空串"))

# 60. 批量删除工程/任务（破坏性）
add("/api/task/deleteMany", "post", "批量删除工程/任务",
    body([("idList", {"type": "array", "items": {"type": "integer"}},
           "要删除的工程/任务ID列表，如 [48314]")], ["idList"]),
    ["Task"],
    description='破坏性操作：删除后不可恢复，一次可删多个工程。响应为 {"status":true} 形式，'
                "status=false 表示删除失败。只删一个工程时也可以用 task delete-one。",
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "是否删除成功"},
    }}))


# ---------------------------------------------------------------------------
# 维度与性能测量对象（61~64）
#
# 注册指标组（测量单元）之前要先有「测量对象」（perf class / MOC）；
# 测量对象的 dimensionNoList 引用「维度」，所以维度要先于对象注册。
# 顺序：维度 → 取 mocId / nmMocId → 注册对象 → 注册指标组 → 注册指标。
# 这几个接口挂在 /bxapi 前缀下，查询参数叫 projectId（值就是 taskId）。

PROJECT_ID_PARAM = query_param("projectId", "integer", True,
                               "工程/任务ID（等同其它接口的 taskId），如 47754")

# {"message":"操作成功","status":true} 形式
PERF_CLASS_RESP = raw_resp({"type": "object", "properties": {
    "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
    "message": {"type": "string", "description": "提示信息，如「操作成功」"},
}})

# 61. 注册维度
add("/bxapi/perf/baseinfo/dimension", "post", "注册维度",
    [PROJECT_ID_PARAM] +
    body([("dimensionId", "string", "维度ID，全局唯一，如 56"),
          ("dimensionName", "string", "维度名称，如 5qi"),
          ("dimensionDataLength", "string", "维度数据长度，如 128"),
          ("stringResId", "string", "语言资源ID，如 AAA_17；用 perf language-resource-add 登记中英文描述"),
          ("microService", {"type": "array",
                            "description": '托管微服务，形如 ["basicBizService","203"]：'
                                           '第一个元素是微服务名，第二个是归属服务ID',
                            "items": {"type": "string"}}, "托管微服务"),
          ("CheckMode", "string", "校验模式，通常填 ALL（注意首字母大写，后端字段名如此）"),
          ("belongService", "string", "归属服务ID，如 \"203\"（由 overallview micro-service-list 查得）")],
         ["dimensionId", "dimensionName", "belongService"]),
    ["Perf"],
    description="注册一个维度，供测量对象的 dimensionNoList/dimensionNoListArray 引用。"
                "维度ID要先规划好（不像 mocId/muId 有自动取号接口）。"
                "请求体里的 belongService 是字符串（如 \"203\"），未列出的后端字段原样透传。"
                "响应 status=false 表示业务失败。",
    responses=PERF_CLASS_RESP)

# 62. 批量删除维度（破坏性）
add("/bxapi/perf/baseinfo/dimension/multiDel", "delete", "批量删除维度",
    [PROJECT_ID_PARAM] +
    body([("dimensionList",
           arr_of(obj([("dimensionId", "string", "维度ID，如 \"56\""),
                       ("belongService", "integer", "归属服务ID，如 203")],
                      ["dimensionId", "belongService"], "要删除的维度"),
                  "要删除的维度列表"),
           "要删除的维度列表")],
         ["dimensionList"]),
    ["Perf"],
    description="破坏性操作（HTTP DELETE + 请求体）：先删掉引用该维度的测量对象，再删维度。"
                "注意 dimensionId 是字符串、belongService 是数字，与注册时的类型不完全一致。"
                "响应 status=false 表示业务失败。",
    responses=PERF_CLASS_RESP)

# 63. 注册（新增/修改）性能测量对象
add("/bxapi/perf/class/{belongService}/{mocId}", "put", "注册性能测量对象（perf class）",
    [path_param("belongService", "integer", "归属服务ID，如 203"),
     path_param("mocId", "integer", "测量对象ID，由 resource auto-gen-id --idType moc 获取"),
     PROJECT_ID_PARAM] +
    body([("mocId", "string", "测量对象ID，需与路径参数一致"),
          ("mocChName", "string", "测量对象中文名，如「指定RATTYPE的SMF N4局向」"),
          ("belongService", "string", "归属服务ID，如 \"203\"，需与路径参数一致"),
          ("realServicesName", "string", "实际服务列表，多个用逗号分隔，如 SmcExecSvc"),
          ("mocType", "integer", "对象类型，如 3"),
          ("dimensionNoList", "string", "维度编号列表的字符串形式，多个用逗号分隔，如 10005"),
          ("dimensionNoListArray", {"type": "array",
                                    "description": "维度编号列表的数组形式，如 [10005]，与 dimensionNoList 一致",
                                    "items": {"type": "integer"}}, "维度编号列表"),
          ("maxMoiNum", "string", "最大实例数，如 4000"),
          ("parentMoc", "string", "父对象，可为 null"),
          ("moiMgrType", "integer", "实例管理方式，如 1"),
          ("stringResId", "string", "语言资源ID，格式 MOC_<测量对象ID>，如 MOC_36"),
          ("monitorId", "string", "监控ID，可为空串"),
          ("nmMocId", "string", "网管测量对象ID，由 resource north-auto-gen-id --idType moc 获取"),
          ("bamMocId", "string", "BAM对象ID，可为空串"),
          ("perfClassMacroDefine", "string", "性能对象宏定义，可为空串"),
          ("objectInstanceName", "string", "对象实例名，可为 null"),
          ("internalCountPara", "string", "内部计数参数，可为 null"),
          ("innerObjRelations", {"type": "array", "description": "内部对象关系，通常为空数组",
                                 "items": {"type": "object"}}, "内部对象关系"),
          ("innerObjListSelected", {"type": "array", "description": "已选内部对象，通常为空数组",
                                    "items": {"type": "object"}}, "已选内部对象"),
          ("microService", {"type": "array",
                            "description": '托管微服务，形如 ["basicBizService","203"]',
                            "items": {"type": "string"}}, "托管微服务"),
          ("realServicesNameList", {"type": "array",
                                    "description": '实际服务列表的数组形式，如 ["SmcExecSvc"]',
                                    "items": {"type": "string"}}, "实际服务列表"),
          ("CheckMode", "string", "校验模式，通常填 ALL（注意首字母大写，后端字段名如此）")],
         ["mocId", "mocChName", "belongService", "stringResId", "nmMocId"]),
    ["Perf"],
    description="新增或修改性能测量对象。调用前要先取到 mocId（resource auto-gen-id --idType moc）"
                "与 nmMocId（resource north-auto-gen-id --idType moc），并注册好 dimensionNoList 引用的维度。"
                "路径上的 belongService/mocId 必须与请求体中的同名字段一致。"
                "注册完对象才能在其上新建指标组（perf indicator-group-add）。"
                "未列出的后端字段原样透传。响应 status=false 表示业务失败。",
    responses=PERF_CLASS_RESP)

# 64. 批量删除性能测量对象（破坏性）
add("/bxapi/perf/class/multiDel", "delete", "批量删除性能测量对象（perf class）",
    [PROJECT_ID_PARAM] +
    body([("perfClassList",
           arr_of(obj([("mocId", "string", "测量对象ID，如 \"36\""),
                       ("belongService", "integer", "归属服务ID，如 203"),
                       ("nmMocId", "integer", "网管测量对象ID，如 1929445387")],
                      ["mocId", "belongService"], "要删除的测量对象"),
                  "要删除的测量对象列表"),
           "要删除的测量对象列表")],
         ["perfClassList"]),
    ["Perf"],
    description="破坏性操作（HTTP DELETE + 请求体）：删除前要先删掉挂在该对象下的指标与指标组，"
                "否则会留下残留引用。响应 status=false 表示业务失败。",
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "是否成功；false 表示业务失败"},
        "data": {"type": "string", "description": "返回数据，通常为空串"},
    }}))

# 65. 拉取 Git 分支并列出微服务
add("/api/autoGit/getMicroServices", "get", "拉取 Git 分支并返回微服务列表",
    [query_param("repositoryUrl", "string", True, "Git 仓库 SSH 地址"),
     query_param("branchName", "string", True, "分支完整引用，如 refs/heads/main"),
     query_param("taskId", "integer", True, "目标工程/任务 ID"),
     query_param("taskName", "string", True, "目标工程/任务名称"),
     query_param("resolveConflict", "integer", True, "是否解决冲突；不解决传 0")],
    ["Task"],
    description="服务端拉取指定 Git 分支，并返回仓库中可供选择的微服务目录。",
    responses=status_resp(
        {"type": "array", "items": {"type": "string"}, "description": "可选微服务列表"},
        "查询结果提示"))

# 66. 选择微服务并列出资源类型
add("/api/autoGit/autoDisplayResource", "post", "选择微服务并返回资源类型",
    body([("microService", "string", "微服务名称，如 ompublic"),
          ("taskId", "integer", "目标工程/任务 ID"),
          ("taskName", "string", "目标工程/任务名称")],
         ["microService", "taskId", "taskName"]),
    ["Task"],
    responses=status_resp(
        {"type": "array", "items": {"type": "string"}, "description": "可导入资源类型列表"},
        "查询结果提示"))

# 67. 判断工程是否为空
add("/myapi/upload/isEmptyProject", "post", "判断工程是否为空",
    body([("taskId", "integer", "目标工程/任务 ID")], ["taskId"]),
    ["Task"],
    description="Git 模型导入前置校验。响应 status 表示校验是否通过。",
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "校验是否通过；false 表示业务失败"},
        "message": {"type": "string", "description": "校验结果提示"},
    }}))

# 68. Git 模型黑名单校验
git_form = obj([
    ("repositoryUrl", "string", "Git 仓库 SSH 地址，如 ssh://git@host:2222/group/project.git"),
    ("branchName", "string", "要导入的分支完整引用，如 refs/heads/main"),
    ("microService", "string", "导入到的微服务名称，如 ompublic"),
    ("importTags", "string", "导入的模型标签，如 perf"),
    ("importModuleTreeJson", "string", "模块树 JSON 字符串；不限制模块时传 []"),
], required=["repositoryUrl", "branchName", "microService", "importTags", "importModuleTreeJson"],
    desc="Git 导入配置")
add("/api/autoGit/blacklistJudge", "post", "Git 模型黑名单校验",
    body([("gitForm", git_form, "Git 仓库与导入范围配置"),
          ("taskId", "integer", "目标工程/任务 ID")],
         ["gitForm", "taskId"]),
    ["Task"],
    description="模型导入前检查所选仓库、分支、微服务及资源类型是否命中黑名单。",
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "校验是否通过；false 表示业务失败"},
        "message": {"type": "string", "description": "校验结果提示"},
    }}))

# 69. 使用 Git 仓库导入 5G 建模模型
add("/api/autoGit/importInfoFormGit", "post", "使用 Git 导入 5G 建模模型",
    body([("gitForm", git_form, "Git 仓库与导入范围配置"),
          ("taskId", "integer", "目标工程/任务 ID"),
          ("taskName", "string", "目标工程/任务名称")],
         ["gitForm", "taskId", "taskName"]),
    ["Task"],
    description="从指定 Git 仓库和分支读取模型，并导入到已有工程。"
                "branchName 使用 refs/heads/... 形式；importModuleTreeJson 是 JSON 字符串，不是数组。"
                "响应 status=false 表示业务失败。",
    responses=raw_resp({"type": "object", "properties": {
        "status": {"type": "boolean", "description": "是否导入成功；false 表示业务失败"},
        "message": {"type": "string", "description": "导入结果提示，如「导入成功」"},
    }}))


swagger = {
    "swagger": "2.0",
    "info": {
        "title": "OMResTool API",
        "version": "1.0.0",
        "description": "OMResTool 建模工具后端接口（由 API 文档生成，供 AI 友好 CLI 使用）",
    },
    "host": "10.243.80.228",
    "basePath": "/",
    "schemes": ["http"],
    "consumes": ["application/json"],
    "produces": ["application/json"],
    "paths": paths,
}

out = os.path.join(os.path.dirname(__file__), "..", "internal", "cli", "docs", "swagger.json")
out = os.path.abspath(out)
with open(out, "w", encoding="utf-8", newline="\n") as f:
    json.dump(swagger, f, ensure_ascii=False, indent=2)
    f.write("\n")  # 末尾换行，避免每次生成都和仓库里的文件差一行

# count operations
n = sum(len(m) for m in paths.values())
print(f"wrote {out}")
print(f"paths: {len(paths)}, operations: {n}")
