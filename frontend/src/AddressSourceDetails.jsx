import React from 'react';

const labels = {
  Full_Name: '全名', Full_Name_Tran: '姓名译文', Gender: '性别', Birthday: '生日', Title: '称谓', Hair_Color: '头发颜色',
  Address: '街道', Address_Alias: '街道别名', Trans_Address: '街道翻译', Trans_Cn_Address: '街道中文', City: '城市', State: '州', State_Full: '州全称', Zip_Code: '邮编', Telephone: '电话号码', Fax: '传真', Temporary_mail: '临时邮箱',
  Credit_Card_Type: '生成卡类型', Credit_Card_Number: '生成卡号', CVV2: '生成安全码', Expires: '生成卡有效期',
  Occupation: '职业', Company_Name: '公司名称', Company_Size: '公司规模', Employment_Status: '就业状态', Monthly_Salary: '月薪', Social_Security_Number: '生成社会保障号', Chain_ID_Card: '生成身份证',
  Username: '用户名', Password: '生成密码', Height: '身高', Weight: '体重', Blood_Type: '血型', System: '操作系统', GUID: 'GUID', Browser_User_Agent: '浏览器 User Agent', Educational_Background: '教育背景', Website: '个人主页', Security_Question: '安全问题', Security_Answer: '问题答案', rowkey: '记录编号',
};

export default function AddressSourceDetails({ data, expanded = false }) {
  const entries = Object.entries(data || {});
  if (!entries.length) return null;
  return <details className="address-source-details" open={expanded}><summary>完整来源资料 · {entries.length} 项</summary><p className="muted">来源网站生成的资料，仅供格式测试；生成的身份、卡号和安全码不代表真实付款资料。</p><dl className="source-fields">{entries.map(([key, value]) => <div key={key}><dt>{labels[key] || key}</dt><dd>{value === '' || value == null ? '来源未提供' : typeof value === 'object' ? JSON.stringify(value) : String(value)}</dd></div>)}</dl></details>;
}
