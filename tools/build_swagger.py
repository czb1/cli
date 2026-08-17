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
PREFIXES = ("/api", "/sbbapi", "/list", "/longtime")


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


def form_param(name, typ, required, desc):
    return {"name": name, "in": "formData", "required": required, "type": typ, "description": desc}


paths = {}


def add(p, method, summary, params, tags, data=None, consumes=None, produces=None):
    entry = {
        "summary": summary,
        "tags": tags,
        "parameters": params,
        "responses": resp(data),
    }
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
          ("neType", "string", "网元类型"),
          ("productType", "string", "产品类型"),
          ("version", "string", "版本号"),
          ("buildVersion", "integer", "构建版本"),
          ("selectedServices", "string", "选中服务"),
          ("gitBranch", "string", "Git分支")], ["taskName"]),
    ["Task"], data=DATA_OBJ)

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
add("/sbbapi/task/getExportStructResult/{taskId}/{taskName}", "post",
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
with open(out, "w", encoding="utf-8") as f:
    json.dump(swagger, f, ensure_ascii=False, indent=2)

# count operations
n = sum(len(m) for m in paths.values())
print(f"wrote {out}")
print(f"paths: {len(paths)}, operations: {n}")
